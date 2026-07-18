package exchange_test

// Adversarial tests for the trading core — the invariants that MUST hold no
// matter what the demo (or a hostile client) throws at the exchange:
//
//   1. No over-fill under concurrency: N takers racing one resting order never
//      fill more than its size; a wallet's money is never double-spent.
//   2. Money conservation: for a buyer with fee=0,
//        available + locked + Σ(qty · avg_cost · MicroPerCent) == deposited.
//      i.e. USDC is never created or destroyed by trading.
//   3. No negative balances ever (the DB CHECKs would surface as errors).
//   4. Replay is idempotent even under concurrency: one signed order, one fill.
//   5. A reverted settle reconciles store + book (money returns, order re-rests).

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// --- harness -----------------------------------------------------------------

type harness struct {
	st  *store.Store
	ex  *exchange.Exchange
	sub *flakySubmitter
	mkt [32]byte
}

// flakySubmitter can be told to fail settles, to exercise revert→reconcile.
type flakySubmitter struct{ fail atomic.Bool }

func (s *flakySubmitter) SettleMatch(context.Context, matching.Fill) (string, error) {
	if s.fail.Load() {
		return "", errors.New("simulated on-chain revert")
	}
	return "", nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(nopWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	st := storetest.Open(t)
	sub := &flakySubmitter{}
	ex := exchange.New(st, ws.NewHub(log), sub, log)
	ex.SettleSync = true // deterministic: offchain settle completes inline

	ctx := context.Background()
	matchID, err := st.UpsertMatch(ctx, "chaos", "A", "B", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var mkt [32]byte
	mkt[0], mkt[31] = 0xC1, 0xA0
	if err := st.CreateMarket(ctx, mkt, matchID, "home_win", "binary", "A win", "rule"); err != nil {
		t.Fatal(err)
	}
	return &harness{st: st, ex: ex, sub: sub, mkt: mkt}
}

type wallet struct {
	pk   [32]byte
	priv ed25519.PrivateKey
	b58  string
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var pk [32]byte
	copy(pk[:], pub)
	return wallet{pk: pk, priv: priv, b58: models.PubkeyString(pk)}
}

func (h *harness) signed(w wallet, outcome, side uint8, price uint16, size, salt uint64) *models.Order {
	o := &models.Order{
		Maker: w.pk, MarketID: h.mkt, Outcome: outcome, Side: side,
		Price: price, Size: size, Salt: salt,
	}
	models.SignOrder(o, w.priv)
	return o
}

// walletEquity returns available + locked + Σ(position qty · avg_cost · micro).
// For a pure buyer with fee=0 this must equal total deposited.
func (h *harness) walletEquity(t *testing.T, w wallet) uint64 {
	t.Helper()
	ctx := context.Background()
	bal, err := h.st.GetBalance(ctx, w.b58)
	if err != nil {
		t.Fatal(err)
	}
	total := bal.Available + bal.Locked
	positions, err := h.st.GetPositions(ctx, w.b58)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range positions {
		total += (p.Yes + p.No) * p.AvgCost * models.MicroPerCent
	}
	return total
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// --- 1. concurrency: no over-fill -------------------------------------------

func TestConcurrentTakersNeverOverfill(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// One resting SELL of 100 YES @60 (seller holds exactly 100 tokens).
	seller := newWallet(t)
	if err := h.st.GrantTokens(ctx, seller.b58, h.mkt, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := h.ex.SubmitOrder(ctx, h.signed(seller, models.OutcomeYes, models.SideSell, 60, 100, 1)); err != nil {
		t.Fatalf("place resting sell: %v", err)
	}

	// 20 buyers, each funded for 40 shares @65 → 800 shares of demand chase 100.
	const buyers = 20
	const each = 40
	ws := make([]wallet, buyers)
	for i := range ws {
		ws[i] = newWallet(t)
		if _, err := h.st.Deposit(ctx, ws[i].b58, 100_000_000); err != nil {
			t.Fatal(err)
		}
	}

	var totalFilled atomic.Uint64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, fills, _, err := h.ex.SubmitOrder(ctx, h.signed(ws[i], models.OutcomeYes, models.SideBuy, 65, each, uint64(100+i)))
			if err != nil {
				return
			}
			for _, f := range fills {
				totalFilled.Add(f.Size)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := totalFilled.Load(); got != 100 {
		t.Fatalf("total filled = %d, want exactly 100 (the resting size)", got)
	}

	// Seller sold exactly 100; token position drained to 0, USDC == 100·60¢.
	sp, _ := h.st.GetPositions(ctx, seller.b58)
	if len(sp) != 1 || sp[0].Yes != 0 || sp[0].YesLocked != 0 {
		t.Errorf("seller position not fully drained: %+v", sp)
	}
	sb, _ := h.st.GetBalance(ctx, seller.b58)
	if sb.Available != 100*60*models.MicroPerCent {
		t.Errorf("seller USDC = %d, want %d", sb.Available, 100*60*models.MicroPerCent)
	}

	// Buyers collectively hold exactly 100 YES; nobody went negative.
	var buyerShares uint64
	for _, w := range ws {
		bp, _ := h.st.GetPositions(ctx, w.b58)
		for _, p := range bp {
			buyerShares += p.Yes
		}
	}
	if buyerShares != 100 {
		t.Errorf("buyers hold %d YES total, want 100 (no phantom shares)", buyerShares)
	}
}

// --- 2 & 3. money conservation + no-negative under a random-ish flood --------

func TestMoneyConservedAcrossManyFills(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// A market maker rests deep two-sided liquidity via MINT (BUY YES + BUY NO
	// at complementary prices), funded generously.
	mm := newWallet(t)
	if _, err := h.st.Deposit(ctx, mm.b58, 10_000_000_000); err != nil {
		t.Fatal(err)
	}
	// Rest BUY NO @40 (so a taker BUY YES @60 mints).
	for i := 0; i < 5; i++ {
		if _, _, _, err := h.ex.SubmitOrder(ctx, h.signed(mm, models.OutcomeNo, models.SideBuy, 40, 200, uint64(1+i))); err != nil {
			t.Fatalf("mm rest: %v", err)
		}
	}

	// A buyer deposits a fixed bank and fires many BUY YES @60 (all fill at
	// their own limit via MINT, so avg_cost is exactly 60 — no rounding).
	buyer := newWallet(t)
	const bank = uint64(500_000_000)
	if _, err := h.st.Deposit(ctx, buyer.b58, bank); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		_, _, _, err := h.ex.SubmitOrder(ctx, h.signed(buyer, models.OutcomeYes, models.SideBuy, 60, 50, uint64(1000+i)))
		if err != nil && !errors.Is(err, store.ErrInsufficientFunds) {
			t.Fatalf("buyer order %d: %v", i, err)
		}
	}

	// Conservation: nothing created or destroyed for the buyer.
	if eq := h.walletEquity(t, buyer); eq != bank {
		t.Fatalf("buyer equity = %d, want %d (Δ=%d) — USDC created/destroyed by trading",
			eq, bank, int64(eq)-int64(bank))
	}

	// No wallet holds a negative balance (would have surfaced as a CHECK error,
	// but assert available never underflowed into a huge uint).
	for _, w := range []wallet{mm, buyer} {
		b, _ := h.st.GetBalance(ctx, w.b58)
		if b.Available > 20_000_000_000 || b.Locked > 20_000_000_000 {
			t.Errorf("implausible balance for %s: %+v (underflow?)", w.b58[:6], b)
		}
	}
}

// --- 4. replay idempotency under concurrency --------------------------------

func TestConcurrentReplayFillsOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	seller := newWallet(t)
	h.st.GrantTokens(ctx, seller.b58, h.mkt, 500, 0)
	h.ex.SubmitOrder(ctx, h.signed(seller, models.OutcomeYes, models.SideSell, 50, 500, 1))

	buyer := newWallet(t)
	h.st.Deposit(ctx, buyer.b58, 100_000_000)
	order := h.signed(buyer, models.OutcomeYes, models.SideBuy, 55, 30, 7) // fixed salt

	// Fire the SAME signed order from 8 goroutines at once.
	var ok, dup atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := h.ex.SubmitOrder(ctx, order)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, store.ErrDuplicateOrder), errors.Is(err, matching.ErrDuplicate):
				dup.Add(1)
			}
		}()
	}
	wg.Wait()

	if ok.Load() != 1 {
		t.Fatalf("replay: %d accepts, want exactly 1 (rest were %d dups)", ok.Load(), dup.Load())
	}
	// Buyer holds exactly 30 — not 30×accepts.
	bp, _ := h.st.GetPositions(ctx, buyer.b58)
	if len(bp) != 1 || bp[0].Yes != 30 {
		t.Fatalf("buyer shares = %v, want 30 (replay double-filled)", bp)
	}
}

// --- 5. reverted settle reconciles ------------------------------------------

func TestRevertReconcilesMoneyAndBook(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.sub.fail.Store(true) // every settle reverts

	seller := newWallet(t)
	h.st.GrantTokens(ctx, seller.b58, h.mkt, 100, 0)
	h.ex.SubmitOrder(ctx, h.signed(seller, models.OutcomeYes, models.SideSell, 60, 100, 1))

	buyer := newWallet(t)
	h.st.Deposit(ctx, buyer.b58, 100_000_000)
	_, fills, _, err := h.ex.SubmitOrder(ctx, h.signed(buyer, models.OutcomeYes, models.SideBuy, 65, 40, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 {
		t.Fatalf("expected a fill to then revert, got %d", len(fills))
	}

	// After revert: buyer's lock is restored, nothing spent, no shares.
	b, _ := h.st.GetBalance(ctx, buyer.b58)
	wantLock := models.BuyCost(65, 40, 0)
	if b.Available != 100_000_000-wantLock || b.Locked != wantLock {
		t.Errorf("buyer after revert: %+v (want available=%d locked=%d)",
			b, 100_000_000-wantLock, wantLock)
	}
	bp, _ := h.st.GetPositions(ctx, buyer.b58)
	for _, p := range bp {
		if p.Yes != 0 {
			t.Errorf("buyer holds %d YES after revert — should be 0", p.Yes)
		}
	}

	// The seller's order re-rests: a fresh taker (with settles working now) fills it.
	h.sub.fail.Store(false)
	carol := newWallet(t)
	h.st.Deposit(ctx, carol.b58, 100_000_000)
	_, fills2, _, err := h.ex.SubmitOrder(ctx, h.signed(carol, models.OutcomeYes, models.SideBuy, 65, 40, 3))
	if err != nil {
		t.Fatal(err)
	}
	if len(fills2) != 1 || fills2[0].Size != 40 {
		t.Fatalf("restored order must fill again: %+v", fills2)
	}
}

// Sanity: the harness itself round-trips a clean trade (guards against the
// whole suite silently passing because SubmitOrder no-ops).
func TestHarnessCleanTrade(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seller := newWallet(t)
	h.st.GrantTokens(ctx, seller.b58, h.mkt, 10, 0)
	h.ex.SubmitOrder(ctx, h.signed(seller, models.OutcomeYes, models.SideSell, 50, 10, 1))
	buyer := newWallet(t)
	h.st.Deposit(ctx, buyer.b58, 100_000_000)
	_, fills, _, err := h.ex.SubmitOrder(ctx, h.signed(buyer, models.OutcomeYes, models.SideBuy, 55, 10, 2))
	if err != nil || len(fills) != 1 {
		t.Fatalf("clean trade failed: %v %v", fills, err)
	}
	fmt.Fprint(nopWriter{}, "ok")
}
