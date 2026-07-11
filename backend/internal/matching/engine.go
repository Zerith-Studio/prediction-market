// Package matching implements the in-memory, price-time-priority order book per
// market. It produces NORMAL/MINT/MERGE matches (interface-contract.md §4) for the
// crank to settle on-chain. The book here is UX-only soft state — OrderStatus on
// chain is the source of truth for fills (interface-contract.md §6.2).
package matching

import (
	"container/heap"
	"sync"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// RestingOrder is a live order sitting in a market's book.
type RestingOrder struct {
	Order     *models.Order
	Hash      [32]byte
	Remaining uint64
	Seq       uint64 // creation sequence, breaks price ties (time priority)
}

// Fill is one matched trade produced by the engine for a single settle_match leg.
type Fill struct {
	MarketID  [32]byte
	Taker     *RestingOrder
	Maker     *RestingOrder
	Price     uint16
	Size      uint64
	MatchType models.MatchType
}

// Book is a single market's YES/NO order book, split by outcome+side.
// BUY YES @ p is economically equivalent to SELL NO @ (100-p); for the scaffold we
// keep outcome books separate and let matchType classification (below) collapse
// NORMAL vs MINT vs MERGE crossings. Full cross-outcome matching is a TODO.
type Book struct {
	mu       sync.Mutex
	MarketID [32]byte

	yesBids *orderHeap // BUY YES, highest price first
	yesAsks *orderHeap // SELL YES, lowest price first
	noBids  *orderHeap
	noAsks  *orderHeap

	nextSeq uint64
}

func NewBook(marketID [32]byte) *Book {
	return &Book{
		MarketID: marketID,
		yesBids:  newOrderHeap(true),
		yesAsks:  newOrderHeap(false),
		noBids:   newOrderHeap(true),
		noAsks:   newOrderHeap(false),
	}
}

// Submit adds an order to the book and greedily matches it against the resting
// opposite side, returning any fills produced. TODO: MINT (two BUY orders on
// opposite outcomes crossing 100) and MERGE (two SELL orders crossing) per
// docs/adr/0002; only NORMAL (direct opposite-side cross) is implemented here.
func (b *Book) Submit(o *models.Order) []Fill {
	b.mu.Lock()
	defer b.mu.Unlock()

	hash := models.OrderHash(o)
	resting := &RestingOrder{Order: o, Hash: hash, Remaining: o.Size, Seq: b.nextSeq}
	b.nextSeq++

	bids, asks := b.sidesFor(o.Outcome)
	var fills []Fill

	if o.Side == models.SideBuy {
		fills = crossAgainst(resting, asks, o.Outcome, b.MarketID)
		if resting.Remaining > 0 {
			heap.Push(bids, resting)
		}
	} else {
		fills = crossAgainst(resting, bids, o.Outcome, b.MarketID)
		if resting.Remaining > 0 {
			heap.Push(asks, resting)
		}
	}
	return fills
}

func (b *Book) sidesFor(outcome uint8) (bids, asks *orderHeap) {
	if outcome == models.OutcomeYes {
		return b.yesBids, b.yesAsks
	}
	return b.noBids, b.noAsks
}

// crossAgainst matches `taker` against the resting opposite-side heap while prices
// cross, mutating Remaining on both sides and popping exhausted resting orders.
func crossAgainst(taker *RestingOrder, opposite *orderHeap, outcome uint8, marketID [32]byte) []Fill {
	var fills []Fill
	for taker.Remaining > 0 && opposite.Len() > 0 {
		best := (*opposite.orders)[0]
		if !pricesCross(taker, best) {
			break
		}
		size := min(taker.Remaining, best.Remaining)
		fills = append(fills, Fill{
			MarketID:  marketID,
			Taker:     taker,
			Maker:     best,
			Price:     best.Order.Price, // resting maker sets the price
			Size:      size,
			MatchType: models.MatchNormal,
		})
		taker.Remaining -= size
		best.Remaining -= size
		if best.Remaining == 0 {
			heap.Pop(opposite)
		}
	}
	return fills
}

func pricesCross(taker, resting *RestingOrder) bool {
	if taker.Order.Side == models.SideBuy {
		return taker.Order.Price >= resting.Order.Price
	}
	return taker.Order.Price <= resting.Order.Price
}
