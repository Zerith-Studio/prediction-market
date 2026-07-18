// Package exchange is the trading core: signed-order intake → in-memory match →
// Postgres mirror → crank settlement → WS events. The REST API and the MM bot
// both submit through here, so every order takes the identical path
// (interface-contract.md §1 rules enforced at entry, chain re-verifies).
package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

var ErrBadSignature = errors.New("exchange: ed25519 signature does not verify")

type Exchange struct {
	store *store.Store
	hub   *ws.Hub
	crank *crank.Crank
	log   *slog.Logger

	mu    sync.Mutex
	books map[[32]byte]*matching.Book

	// settleAsync submits fills to the crank without blocking the request
	// (tests can flip to synchronous to assert outcomes deterministically).
	SettleSync bool
}

// New wires the trading core. The crank's hooks are installed here so
// confirm/revert always reconcile store + book + WS together.
func New(st *store.Store, hub *ws.Hub, submitter crank.Submitter, log *slog.Logger) *Exchange {
	e := &Exchange{
		store: st,
		hub:   hub,
		log:   log,
		books: make(map[[32]byte]*matching.Book),
	}
	e.crank = crank.New(submitter, crank.Hooks{
		OnConfirmed: e.onSettleConfirmed,
		OnReverted:  e.onSettleReverted,
	}, log)
	return e
}

func (e *Exchange) book(marketID [32]byte) *matching.Book {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.books[marketID]
	if !ok {
		b = matching.NewBook(marketID)
		e.books[marketID] = b
	}
	return b
}

// RestoreBooks rebuilds the in-memory books from the Postgres mirror (restart
// path — resting orders were non-crossing when persisted, so no re-matching).
func (e *Exchange) RestoreBooks(ctx context.Context) error {
	markets, err := e.store.ListMarkets(ctx, "open")
	if err != nil {
		return err
	}
	for _, m := range markets {
		rows, err := e.store.LiveOrders(ctx, m.MarketID)
		if err != nil {
			return err
		}
		b := e.book(m.MarketID)
		for _, r := range rows {
			o := r.Order
			b.LoadResting(&o, r.Remaining)
		}
		if len(rows) > 0 {
			e.log.Info("exchange: book restored", "market", models.HashString(m.MarketID), "orders", len(rows))
		}
	}
	return nil
}

// SubmitOrder is the single entry point for every signed order (users via REST,
// the MM bot directly): verify the ed25519 signature (defense in depth — E1
// re-verifies on-chain), soft-lock collateral in Postgres, cross in memory,
// mirror the fills, then crank each fill to the chain. The third return value is
// the hashes of the caller's OWN resting orders cancelled by self-trade
// prevention (see matching.Book.Submit) — nil unless the order self-crossed.
func (e *Exchange) SubmitOrder(ctx context.Context, o *models.Order) ([32]byte, []matching.Fill, [][32]byte, error) {
	hash := models.OrderHash(o)
	if !models.VerifyOrderSig(o) {
		return hash, nil, nil, ErrBadSignature
	}

	// Postgres first: entry-collateral rules + replay protection live in one
	// transaction. If the engine then rejects (validation), release via cancel.
	if err := e.store.PlaceOrder(ctx, o); err != nil {
		return hash, nil, nil, err
	}

	book := e.book(o.MarketID)
	fills, stpCancelled, err := book.Submit(o)
	if err != nil {
		// The order never entered the book — undo the placement + lock.
		if cerr := e.store.CancelOrder(ctx, hash, o.Maker); cerr != nil {
			e.log.Error("exchange: rollback place after engine reject", "err", cerr)
		}
		return hash, nil, nil, err
	}

	// Self-trade prevention: the engine cancelled these of the taker's OWN resting
	// orders rather than let it fill against itself (cancel-resting). Release each
	// order's soft-lock in Postgres and tell WS subscribers — same unwind as a user
	// cancel. The owner is o.Maker by construction (STP only ever cancels self).
	for _, ch := range stpCancelled {
		if cerr := e.store.CancelOrder(ctx, ch, o.Maker); cerr != nil {
			e.log.Error("exchange: release self-trade-prevented order", "err", cerr)
		}
		e.hub.Broadcast(ws.Event{
			Type:     ws.EventOrder,
			MarketID: models.HashString(o.MarketID),
			Data: map[string]any{
				"order_hash": models.HashString(ch),
				"status":     "cancelled",
				"reason":     "self_trade_prevention",
			},
		})
	}

	var fillIDs []string
	var applied []matching.Fill
	for _, f := range fills {
		id, err := e.store.ApplyFill(ctx, f)
		if err != nil {
			// Mirror rejected the fill (e.g. the index synced this order down to
			// chain truth after a late-landing settle). Unwind the engine's
			// optimistic decrement so memory and DB stay consistent — the fill
			// simply never happened.
			e.log.Error("exchange: ApplyFill rejected — unwinding engine fill", "err", err)
			book.Unfill(f.Taker.Hash, f.Size)
			book.Unfill(f.Maker.Hash, f.Size)
			continue
		}
		fillIDs = append(fillIDs, id)
		applied = append(applied, f)
	}

	e.broadcastOrderEvents(o, hash, fills)

	settle := func() {
		e.crank.Settle(context.WithoutCancel(ctx), fillIDs, applied)
	}
	if e.SettleSync {
		settle()
	} else if len(fillIDs) > 0 {
		go settle()
	}
	return hash, fills, stpCancelled, nil
}

// Cancel removes a live order everywhere: Postgres (releases the soft-lock),
// the in-memory book, and tells WS subscribers. On-chain cancel_order is the
// user's own tx (maker-signed) — this is the off-chain book removal.
func (e *Exchange) Cancel(ctx context.Context, hash [32]byte, maker [32]byte) error {
	if err := e.store.CancelOrder(ctx, hash, maker); err != nil {
		return err
	}
	// Find the book lazily — cancel is market-agnostic in the API surface.
	e.mu.Lock()
	books := make([]*matching.Book, 0, len(e.books))
	for _, b := range e.books {
		books = append(books, b)
	}
	e.mu.Unlock()
	var marketID [32]byte
	for _, b := range books {
		if b.Cancel(hash) {
			marketID = b.MarketID
			break
		}
	}
	e.hub.Broadcast(ws.Event{
		Type:     ws.EventOrder,
		MarketID: models.HashString(marketID),
		Data: map[string]any{
			"order_hash": models.HashString(hash),
			"status":     "cancelled",
		},
	})
	e.broadcastBook(marketID)
	return nil
}

// Book returns the aggregated read view (GET /markets/:id/book).
func (e *Exchange) Book(marketID [32]byte) matching.Snapshot {
	return e.book(marketID).Snapshot()
}

func (e *Exchange) broadcastOrderEvents(o *models.Order, hash [32]byte, fills []matching.Fill) {
	marketHex := models.HashString(o.MarketID)
	for _, f := range fills {
		e.hub.Broadcast(ws.Event{
			Type:     ws.EventFill,
			MarketID: marketHex,
			Data: map[string]any{
				"taker_hash": models.HashString(f.Taker.Hash),
				"maker_hash": models.HashString(f.Maker.Hash),
				"price":      f.Price,
				"size":       f.Size,
				"match_type": f.MatchType,
			},
		})
	}
	status := "live"
	if len(fills) > 0 && fills[len(fills)-1].Taker.Remaining == 0 {
		status = "matched"
	}
	e.hub.Broadcast(ws.Event{
		Type:     ws.EventOrder,
		MarketID: marketHex,
		Data: map[string]any{
			"order_hash": models.HashString(hash),
			"status":     status,
		},
	})
	e.broadcastBook(o.MarketID)
}

func (e *Exchange) broadcastBook(marketID [32]byte) {
	e.hub.Broadcast(ws.Event{
		Type:     ws.EventBookUpdate,
		MarketID: models.HashString(marketID),
		Data:     e.Book(marketID),
	})
}

// onSettleConfirmed: record the tx signature and let the UI flip the fill to
// "Verified on Solana".
func (e *Exchange) onSettleConfirmed(ctx context.Context, fillID, txSig string) {
	if err := e.store.SetFillSettleTx(ctx, fillID, txSig); err != nil {
		e.log.Error("exchange: record settle tx", "fill", fillID, "err", err)
	}
	e.hub.Broadcast(ws.Event{
		Type: ws.EventFill,
		Data: map[string]any{"fill_id": fillID, "settle_tx": txSig, "status": "settled"},
	})
}

// onSettleReverted: the §6.2 revert→reconcile path — unwind Postgres, restore
// the book, re-emit order_update so the losing race is a no-op.
func (e *Exchange) onSettleReverted(ctx context.Context, fillID string, f matching.Fill) {
	if err := e.store.RevertFill(ctx, fillID, f); err != nil {
		e.log.Error("exchange: revert fill mirror", "fill", fillID, "err", err)
	}
	book := e.book(f.MarketID)
	book.Unfill(f.Taker.Hash, f.Size)
	book.Unfill(f.Maker.Hash, f.Size)
	for _, h := range [][32]byte{f.Taker.Hash, f.Maker.Hash} {
		e.hub.Broadcast(ws.Event{
			Type:     ws.EventOrder,
			MarketID: models.HashString(f.MarketID),
			Data: map[string]any{
				"order_hash": models.HashString(h),
				"status":     "live",
				"reverted":   fillID,
			},
		})
	}
	e.broadcastBook(f.MarketID)
}

// Describe keeps fmt behavior stable if anyone prints the exchange.
func (e *Exchange) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return fmt.Sprintf("exchange(%d books)", len(e.books))
}
