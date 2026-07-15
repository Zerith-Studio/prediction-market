// Package matching implements the in-memory, price-time-priority order book per
// market. It produces NORMAL/MINT/MERGE matches (interface-contract.md §4, ADR 0002)
// for the crank to settle on-chain. The book here is UX-only soft state — OrderStatus
// on chain is the source of truth for fills (interface-contract.md §6.2).
package matching

import (
	"container/heap"
	"errors"
	"sync"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// RestingOrder is a live order sitting in a market's book.
type RestingOrder struct {
	Order     *models.Order
	Hash      [32]byte
	Remaining uint64
	Seq       uint64 // creation sequence, breaks price ties (time priority)
	cancelled bool
}

// Fill is one matched trade produced by the engine for a single settle_match leg.
//
// Price semantics follow the on-chain program (programs/pitchmarket/src/lib.rs):
//   - NORMAL: Price is the execution price (the resting maker's limit, in the shared
//     outcome's terms) — the chain moves Price×Size USDC between buyer and seller.
//   - MINT/MERGE: the chain moves each order's OWN limit price (taker pays/receives
//     taker.Price, maker pays/receives maker.Price; any complete-set surplus stays in
//     the pool). Price here carries the taker's limit — informational only.
type Fill struct {
	MarketID  [32]byte
	Taker     *RestingOrder
	Maker     *RestingOrder
	Price     uint16
	Size      uint64
	MatchType models.MatchType
}

var (
	ErrBadPrice   = errors.New("matching: price must be 1..99")
	ErrBadSize    = errors.New("matching: size must be > 0")
	ErrSizeTooBig = errors.New("matching: size exceeds the sane maximum")
	ErrExpired    = errors.New("matching: order already expired")
	ErrDuplicate  = errors.New("matching: order hash already seen (replay)")
	ErrBadOutcome = errors.New("matching: outcome must be 0 or 1")
	ErrBadSide    = errors.New("matching: side must be 0 (BUY) or 1 (SELL)")
)

// Book is a single market's YES/NO order book, split by outcome+side. A taker on
// outcome X crosses two resting populations (ADR 0002 three match types):
//
//	BUY X  → SELL X asks (NORMAL) and BUY (1−X) bids (MINT, effective ask 100−p)
//	SELL X → BUY X bids (NORMAL)  and SELL (1−X) asks (MERGE, effective bid 100−p)
//
// Candidates from both populations are merged by best-effective-price-first,
// ties broken by earlier Seq.
type Book struct {
	mu       sync.Mutex
	MarketID [32]byte

	bids [2]*orderHeap // BUY resting, per outcome, highest price first
	asks [2]*orderHeap // SELL resting, per outcome, lowest price first

	byHash  map[[32]byte]*RestingOrder
	nextSeq uint64
	now     func() time.Time
}

func NewBook(marketID [32]byte) *Book {
	return &Book{
		MarketID: marketID,
		bids:     [2]*orderHeap{newOrderHeap(true), newOrderHeap(true)},
		asks:     [2]*orderHeap{newOrderHeap(false), newOrderHeap(false)},
		byHash:   make(map[[32]byte]*RestingOrder),
		now:      time.Now,
	}
}

func validate(o *models.Order, now time.Time) error {
	if o.Price < 1 || o.Price > 99 {
		return ErrBadPrice
	}
	if o.Size == 0 {
		return ErrBadSize
	}
	if o.Size > models.MaxOrderSize {
		return ErrSizeTooBig
	}
	if o.Outcome > 1 {
		return ErrBadOutcome
	}
	if o.Side > 1 {
		return ErrBadSide
	}
	if o.Expiry != 0 && o.Expiry <= now.Unix() {
		return ErrExpired
	}
	return nil
}

// Submit validates and adds an order to the book, greedily matching it against
// both crossing populations, and returns any fills produced. The unfilled
// remainder rests (GTC / GTD per Expiry).
func (b *Book) Submit(o *models.Order) ([]Fill, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if err := validate(o, now); err != nil {
		return nil, err
	}
	hash := models.OrderHash(o)
	if _, dup := b.byHash[hash]; dup {
		return nil, ErrDuplicate
	}

	taker := &RestingOrder{Order: o, Hash: hash, Remaining: o.Size, Seq: b.nextSeq}
	b.nextSeq++

	fills := b.cross(taker, now)

	b.byHash[hash] = taker
	if taker.Remaining > 0 {
		if o.Side == models.SideBuy {
			heap.Push(b.bids[o.Outcome], taker)
		} else {
			heap.Push(b.asks[o.Outcome], taker)
		}
	}
	return fills, nil
}

// LoadResting inserts an order into the book WITHOUT matching — used to rebuild
// the in-memory book from the Postgres mirror on restart (resting orders were
// already non-crossing when persisted).
func (b *Book) LoadResting(o *models.Order, remaining uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	hash := models.OrderHash(o)
	if _, dup := b.byHash[hash]; dup || remaining == 0 {
		return
	}
	r := &RestingOrder{Order: o, Hash: hash, Remaining: remaining, Seq: b.nextSeq}
	b.nextSeq++
	b.byHash[hash] = r
	if o.Side == models.SideBuy {
		heap.Push(b.bids[o.Outcome], r)
	} else {
		heap.Push(b.asks[o.Outcome], r)
	}
}

// Cancel marks the order dead; its heap entry is skipped lazily. Returns false
// if the hash is unknown or the order is already fully filled/cancelled.
func (b *Book) Cancel(hash [32]byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.byHash[hash]
	if !ok || r.cancelled || r.Remaining == 0 {
		return false
	}
	r.cancelled = true
	return true
}

// Unfill rolls back a fill's optimistic Remaining decrement after the
// corresponding settle_match REVERTED on-chain (interface-contract.md §6.2
// revert→reconcile): the fill never happened, so `size` returns to the order.
func (b *Book) Unfill(hash [32]byte, size uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.byHash[hash]
	if !ok {
		return
	}
	wasResting := r.Remaining > 0 && !r.cancelled
	r.Remaining += size
	if !wasResting && !r.cancelled {
		if r.Order.Side == models.SideBuy {
			heap.Push(b.bids[r.Order.Outcome], r)
		} else {
			heap.Push(b.asks[r.Order.Outcome], r)
		}
	}
}

// candidate is one resting population a taker can cross, with the price mapping
// that makes its orders comparable to the taker's limit.
type candidate struct {
	h         *orderHeap
	matchType models.MatchType
	// effective converts the resting maker's limit into the taker's outcome terms:
	// identity for NORMAL, 100−p for MINT/MERGE (complete-set complement).
	effective func(makerPrice uint16) uint16
}

func (b *Book) cross(taker *RestingOrder, now time.Time) []Fill {
	o := taker.Order
	var cands [2]candidate
	takerBuys := o.Side == models.SideBuy
	if takerBuys {
		cands = [2]candidate{
			{b.asks[o.Outcome], models.MatchNormal, func(p uint16) uint16 { return p }},
			{b.bids[1-o.Outcome], models.MatchMint, func(p uint16) uint16 { return 100 - p }},
		}
	} else {
		cands = [2]candidate{
			{b.bids[o.Outcome], models.MatchNormal, func(p uint16) uint16 { return p }},
			{b.asks[1-o.Outcome], models.MatchMerge, func(p uint16) uint16 { return 100 - p }},
		}
	}

	var fills []Fill
	for taker.Remaining > 0 {
		best := -1
		var bestEff uint16
		var bestMaker *RestingOrder
		for i, c := range cands {
			maker := peekLive(c.h, now)
			if maker == nil {
				continue
			}
			eff := c.effective(maker.Order.Price)
			crosses := (takerBuys && o.Price >= eff) || (!takerBuys && o.Price <= eff)
			if !crosses {
				continue
			}
			better := best == -1 ||
				(takerBuys && eff < bestEff) || (!takerBuys && eff > bestEff) ||
				(eff == bestEff && maker.Seq < bestMaker.Seq)
			if better {
				best, bestEff, bestMaker = i, eff, maker
			}
		}
		if best == -1 {
			break
		}

		size := min(taker.Remaining, bestMaker.Remaining)
		price := bestMaker.Order.Price // NORMAL: maker sets the price
		if cands[best].matchType != models.MatchNormal {
			price = o.Price // MINT/MERGE: chain charges each side its own limit
		}
		fills = append(fills, Fill{
			MarketID:  b.MarketID,
			Taker:     taker,
			Maker:     bestMaker,
			Price:     price,
			Size:      size,
			MatchType: cands[best].matchType,
		})
		taker.Remaining -= size
		bestMaker.Remaining -= size
		if bestMaker.Remaining == 0 {
			heap.Pop(cands[best].h)
		}
	}
	return fills
}

// peekLive returns the best live resting order on h, lazily popping cancelled
// and expired entries.
func peekLive(h *orderHeap, now time.Time) *RestingOrder {
	for h.Len() > 0 {
		r := (*h.orders)[0]
		if r.cancelled || r.Remaining == 0 ||
			(r.Order.Expiry != 0 && r.Order.Expiry <= now.Unix()) {
			heap.Pop(h)
			continue
		}
		return r
	}
	return nil
}

// Level is one aggregated price level of the book read view.
type Level struct {
	Price uint16 `json:"price"`
	Size  uint64 `json:"size"`
}

// Snapshot is the REST/WS read view of the book (GET /markets/:id/book).
type Snapshot struct {
	// Indexed by outcome (0 = NO, 1 = YES). Bids sorted best (highest) first,
	// asks best (lowest) first.
	Bids [2][]Level `json:"bids"`
	Asks [2][]Level `json:"asks"`
}

func (b *Book) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	var s Snapshot
	for outcome := 0; outcome < 2; outcome++ {
		s.Bids[outcome] = aggregate(b.bids[outcome], now, true)
		s.Asks[outcome] = aggregate(b.asks[outcome], now, false)
	}
	return s
}

func aggregate(h *orderHeap, now time.Time, descending bool) []Level {
	bySize := make(map[uint16]uint64)
	for _, r := range *h.orders {
		if r.cancelled || r.Remaining == 0 ||
			(r.Order.Expiry != 0 && r.Order.Expiry <= now.Unix()) {
			continue
		}
		bySize[r.Order.Price] += r.Remaining
	}
	levels := make([]Level, 0, len(bySize))
	for p, sz := range bySize {
		levels = append(levels, Level{Price: p, Size: sz})
	}
	sortLevels(levels, descending)
	return levels
}

func sortLevels(levels []Level, descending bool) {
	for i := 1; i < len(levels); i++ {
		for j := i; j > 0; j-- {
			if (descending && levels[j].Price > levels[j-1].Price) ||
				(!descending && levels[j].Price < levels[j-1].Price) {
				levels[j], levels[j-1] = levels[j-1], levels[j]
			} else {
				break
			}
		}
	}
}
