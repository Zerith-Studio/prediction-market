// Package index mirrors on-chain state into Postgres so UI reads never hit RPC
// (PROJECT_PLAN §3). The chain is authoritative for fills (OrderStatus PDAs);
// orders.remaining in Postgres is this mirror. Source is an interface: the
// polling RPC implementation runs once the program is deployed; tests push
// events through a fake.
package index

import (
	"context"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

// OrderStatusEvent is one observed on-chain OrderStatus PDA state.
type OrderStatusEvent struct {
	OrderHash [32]byte
	Remaining uint64
	Closed    bool // is_filled_or_cancelled
}

// Source streams chain observations. Implementations: RPCPoller (below), fakes.
type Source interface {
	Events(ctx context.Context) (<-chan OrderStatusEvent, error)
}

type Processor struct {
	store *store.Store
	log   *slog.Logger
}

func NewProcessor(st *store.Store, log *slog.Logger) *Processor {
	return &Processor{store: st, log: log}
}

// Run applies every observation to the mirror until the source closes.
func (p *Processor) Run(ctx context.Context, src Source) error {
	events, err := src.Events(ctx)
	if err != nil {
		return err
	}
	for ev := range events {
		if err := p.store.SyncOrderChainState(ctx, ev.OrderHash, ev.Remaining, ev.Closed); err != nil {
			p.log.Error("index: sync order", "hash", models.HashString(ev.OrderHash), "err", err)
		}
	}
	return nil
}

// RPCPoller polls getProgramAccounts for OrderStatus PDAs. Layout per
// programs/pitchmarket/src/state.rs OrderStatus: disc(8) ‖ order_hash(32) ‖
// remaining u64 LE ‖ is_filled_or_cancelled u8 ‖ bump u8.
type RPCPoller struct {
	Client    *rpc.Client
	ProgramID solana.PublicKey
	Every     time.Duration
	log       *slog.Logger
}

func NewRPCPoller(rpcURL string, programID solana.PublicKey, log *slog.Logger) *RPCPoller {
	return &RPCPoller{Client: rpc.New(rpcURL), ProgramID: programID, Every: 5 * time.Second, log: log}
}

const orderStatusSpan = 8 + 32 + 8 + 1 + 1

func (p *RPCPoller) Events(ctx context.Context) (<-chan OrderStatusEvent, error) {
	out := make(chan OrderStatusEvent)
	go func() {
		defer close(out)
		t := time.NewTicker(p.Every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.pollOnce(ctx, out)
			}
		}
	}()
	return out, nil
}

func (p *RPCPoller) pollOnce(ctx context.Context, out chan<- OrderStatusEvent) {
	span := uint64(orderStatusSpan)
	res, err := p.Client.GetProgramAccountsWithOpts(ctx, p.ProgramID, &rpc.GetProgramAccountsOpts{
		Filters: []rpc.RPCFilter{{DataSize: span}},
	})
	if err != nil {
		p.log.Warn("index: getProgramAccounts", "err", err)
		return
	}
	for _, acc := range res {
		data := acc.Account.Data.GetBinary()
		if len(data) != orderStatusSpan {
			continue
		}
		var ev OrderStatusEvent
		copy(ev.OrderHash[:], data[8:40])
		ev.Remaining = uint64(data[40]) | uint64(data[41])<<8 | uint64(data[42])<<16 | uint64(data[43])<<24 |
			uint64(data[44])<<32 | uint64(data[45])<<40 | uint64(data[46])<<48 | uint64(data[47])<<56
		ev.Closed = data[48] == 1
		select {
		case out <- ev:
		case <-ctx.Done():
			return
		}
	}
}
