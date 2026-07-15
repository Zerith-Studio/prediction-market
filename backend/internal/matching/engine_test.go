package matching

import (
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

var mkt = [32]byte{1}

func order(maker byte, outcome, side uint8, price uint16, size uint64, salt uint64) *models.Order {
	o := &models.Order{
		MarketID: mkt,
		Outcome:  outcome,
		Side:     side,
		Price:    price,
		Size:     size,
		Salt:     salt,
	}
	o.Maker[0] = maker
	return o
}

func mustSubmit(t *testing.T, b *Book, o *models.Order) []Fill {
	t.Helper()
	fills, err := b.Submit(o)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return fills
}

func TestNormalCross(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeYes, models.SideSell, 60, 100, 1)) // rests
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 40, 2))

	if len(fills) != 1 {
		t.Fatalf("want 1 fill, got %d", len(fills))
	}
	f := fills[0]
	if f.MatchType != models.MatchNormal {
		t.Errorf("want NORMAL, got %v", f.MatchType)
	}
	if f.Price != 60 { // maker sets the price
		t.Errorf("want price 60 (maker limit), got %d", f.Price)
	}
	if f.Size != 40 {
		t.Errorf("want size 40, got %d", f.Size)
	}
	if f.Maker.Remaining != 60 || f.Taker.Remaining != 0 {
		t.Errorf("remaining: maker=%d taker=%d", f.Maker.Remaining, f.Taker.Remaining)
	}
}

func TestNoCrossRests(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeYes, models.SideSell, 60, 100, 1))
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 55, 40, 2))
	if len(fills) != 0 {
		t.Fatalf("bid 55 must not cross ask 60, got %d fills", len(fills))
	}
	snap := b.Snapshot()
	if len(snap.Bids[models.OutcomeYes]) != 1 || snap.Bids[models.OutcomeYes][0].Price != 55 {
		t.Errorf("bid should rest at 55: %+v", snap.Bids[models.OutcomeYes])
	}
}

func TestMintCross(t *testing.T) {
	b := NewBook(mkt)
	// BUY NO @45 resting; BUY YES @65 arrives. 65+45 >= 100 → MINT.
	mustSubmit(t, b, order(1, models.OutcomeNo, models.SideBuy, 45, 50, 1))
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 50, 2))

	if len(fills) != 1 || fills[0].MatchType != models.MatchMint {
		t.Fatalf("want 1 MINT fill, got %+v", fills)
	}
	if fills[0].Price != 65 { // taker's own limit — chain charges each side its limit
		t.Errorf("want taker limit 65, got %d", fills[0].Price)
	}
}

func TestMintNoCrossUnder100(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeNo, models.SideBuy, 30, 50, 1))
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 50, 2))
	if len(fills) != 0 {
		t.Fatalf("65+30 < 100 must not MINT, got %+v", fills)
	}
}

func TestMergeCross(t *testing.T) {
	b := NewBook(mkt)
	// SELL NO @35 resting; SELL YES @60 arrives. 60+35 <= 100 → MERGE.
	mustSubmit(t, b, order(1, models.OutcomeNo, models.SideSell, 35, 20, 1))
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideSell, 60, 20, 2))

	if len(fills) != 1 || fills[0].MatchType != models.MatchMerge {
		t.Fatalf("want 1 MERGE fill, got %+v", fills)
	}
}

func TestMergeNoCrossOver100(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeNo, models.SideSell, 45, 20, 1))
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideSell, 60, 20, 2))
	if len(fills) != 0 {
		t.Fatalf("60+45 > 100 must not MERGE, got %+v", fills)
	}
}

// A taker must take the best effective price across NORMAL and MINT candidates.
func TestBestEffectivePriceWins(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeYes, models.SideSell, 62, 10, 1)) // NORMAL ask, eff 62
	mustSubmit(t, b, order(2, models.OutcomeNo, models.SideBuy, 45, 10, 2))   // MINT bid, eff 55

	fills := mustSubmit(t, b, order(3, models.OutcomeYes, models.SideBuy, 70, 10, 3))
	if len(fills) != 1 || fills[0].MatchType != models.MatchMint {
		t.Fatalf("MINT at eff 55 must beat NORMAL ask 62: %+v", fills)
	}
}

func TestTakerSweepsBothPopulations(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeNo, models.SideBuy, 45, 10, 1))   // eff 55
	mustSubmit(t, b, order(2, models.OutcomeYes, models.SideSell, 62, 10, 2)) // eff 62

	fills := mustSubmit(t, b, order(3, models.OutcomeYes, models.SideBuy, 70, 25, 3))
	if len(fills) != 2 {
		t.Fatalf("want 2 fills, got %d: %+v", len(fills), fills)
	}
	if fills[0].MatchType != models.MatchMint || fills[1].MatchType != models.MatchNormal {
		t.Errorf("want MINT then NORMAL, got %v then %v", fills[0].MatchType, fills[1].MatchType)
	}
	if fills[1].Taker.Remaining != 5 {
		t.Errorf("taker should have 25-10-10=5 remaining, got %d", fills[1].Taker.Remaining)
	}
	snap := b.Snapshot()
	if len(snap.Bids[models.OutcomeYes]) != 1 || snap.Bids[models.OutcomeYes][0].Size != 5 {
		t.Errorf("remainder should rest: %+v", snap.Bids[models.OutcomeYes])
	}
}

func TestPriceTimePriority(t *testing.T) {
	b := NewBook(mkt)
	first := order(1, models.OutcomeYes, models.SideSell, 60, 10, 1)
	second := order(2, models.OutcomeYes, models.SideSell, 60, 10, 2)
	mustSubmit(t, b, first)
	mustSubmit(t, b, second)

	fills := mustSubmit(t, b, order(3, models.OutcomeYes, models.SideBuy, 60, 10, 3))
	if len(fills) != 1 {
		t.Fatalf("want 1 fill, got %d", len(fills))
	}
	if fills[0].Maker.Hash != models.OrderHash(first) {
		t.Error("earlier order at same price must fill first")
	}
}

func TestCancel(t *testing.T) {
	b := NewBook(mkt)
	o := order(1, models.OutcomeYes, models.SideSell, 60, 10, 1)
	mustSubmit(t, b, o)
	if !b.Cancel(models.OrderHash(o)) {
		t.Fatal("cancel of live order must succeed")
	}
	if b.Cancel(models.OrderHash(o)) {
		t.Fatal("double-cancel must fail")
	}
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 10, 2))
	if len(fills) != 0 {
		t.Fatalf("cancelled order must not fill: %+v", fills)
	}
	if len(b.Snapshot().Asks[models.OutcomeYes]) != 0 {
		t.Error("cancelled order must not appear in snapshot")
	}
}

func TestReplayRejected(t *testing.T) {
	b := NewBook(mkt)
	o := order(1, models.OutcomeYes, models.SideSell, 60, 10, 1)
	mustSubmit(t, b, o)
	if _, err := b.Submit(o); err != ErrDuplicate {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestExpiredMakerSkipped(t *testing.T) {
	b := NewBook(mkt)
	fakeNow := time.Unix(1_000_000, 0)
	b.now = func() time.Time { return fakeNow }

	expiring := order(1, models.OutcomeYes, models.SideSell, 60, 10, 1)
	expiring.Expiry = fakeNow.Unix() + 10
	mustSubmit(t, b, expiring)

	fakeNow = fakeNow.Add(20 * time.Second)
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 10, 2))
	if len(fills) != 0 {
		t.Fatalf("expired maker must not fill: %+v", fills)
	}
}

func TestValidation(t *testing.T) {
	b := NewBook(mkt)
	cases := []struct {
		o    *models.Order
		want error
	}{
		{order(1, models.OutcomeYes, models.SideBuy, 0, 10, 1), ErrBadPrice},
		{order(1, models.OutcomeYes, models.SideBuy, 100, 10, 2), ErrBadPrice},
		{order(1, models.OutcomeYes, models.SideBuy, 50, 0, 3), ErrBadSize},
		{order(1, 2, models.SideBuy, 50, 10, 4), ErrBadOutcome},
		{order(1, models.OutcomeYes, 2, 50, 10, 5), ErrBadSide},
	}
	for _, c := range cases {
		if _, err := b.Submit(c.o); err != c.want {
			t.Errorf("want %v, got %v", c.want, err)
		}
	}
	expired := order(1, models.OutcomeYes, models.SideBuy, 50, 10, 6)
	expired.Expiry = time.Now().Unix() - 1
	if _, err := b.Submit(expired); err != ErrExpired {
		t.Errorf("want ErrExpired, got %v", err)
	}
}

func TestRestoreAfterRevert(t *testing.T) {
	b := NewBook(mkt)
	maker := order(1, models.OutcomeYes, models.SideSell, 60, 10, 1)
	mustSubmit(t, b, maker)
	fills := mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 65, 10, 2))
	if len(fills) != 1 || fills[0].Maker.Remaining != 0 {
		t.Fatalf("setup: %+v", fills)
	}

	// settle_match reverted on-chain → unwind the optimistic fill.
	b.Unfill(models.OrderHash(maker), 10)

	fills = mustSubmit(t, b, order(3, models.OutcomeYes, models.SideBuy, 65, 10, 3))
	if len(fills) != 1 || fills[0].Size != 10 {
		t.Fatalf("restored maker must fill again: %+v", fills)
	}
}

func TestLoadRestingDoesNotMatch(t *testing.T) {
	b := NewBook(mkt)
	b.LoadResting(order(1, models.OutcomeYes, models.SideSell, 60, 10, 1), 10)
	b.LoadResting(order(2, models.OutcomeYes, models.SideBuy, 65, 10, 2), 10)
	// Crossing orders loaded from the mirror must NOT self-match on restart —
	// but must still be crossable by a new taker.
	snap := b.Snapshot()
	if len(snap.Asks[models.OutcomeYes]) != 1 || len(snap.Bids[models.OutcomeYes]) != 1 {
		t.Fatalf("both loaded orders must rest: %+v", snap)
	}
}

func TestSnapshotAggregatesLevels(t *testing.T) {
	b := NewBook(mkt)
	mustSubmit(t, b, order(1, models.OutcomeYes, models.SideBuy, 55, 10, 1))
	mustSubmit(t, b, order(2, models.OutcomeYes, models.SideBuy, 55, 15, 2))
	mustSubmit(t, b, order(3, models.OutcomeYes, models.SideBuy, 50, 20, 3))

	levels := b.Snapshot().Bids[models.OutcomeYes]
	if len(levels) != 2 {
		t.Fatalf("want 2 levels, got %+v", levels)
	}
	if levels[0].Price != 55 || levels[0].Size != 25 {
		t.Errorf("best bid level: %+v", levels[0])
	}
	if levels[1].Price != 50 || levels[1].Size != 20 {
		t.Errorf("second level: %+v", levels[1])
	}
}
