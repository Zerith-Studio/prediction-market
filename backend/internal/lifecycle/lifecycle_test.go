package lifecycle_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed/replay"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// The full lifecycle loop against the recorded demo fixture: register →
// replay feed → resolution of every template → precision settle → combo sweep.
func TestFixtureLifecycleEndToEnd(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	rfqSvc := rfq.New(st, hub, nil, log)
	svc := lifecycle.New(st, hub, rfqSvc, nil, nil, log)

	const fixture = "demo-final"
	if err := svc.RegisterFixture(ctx, fixture, "Argentina", "France", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("RegisterFixture: %v", err)
	}
	// Idempotent re-register.
	if err := svc.RegisterFixture(ctx, fixture, "Argentina", "France", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	markets, err := st.ListMarkets(ctx, "open")
	if err != nil || len(markets) != len(templates.Registry) {
		t.Fatalf("want %d open markets, got %d (%v)", len(templates.Registry), len(markets), err)
	}

	// Precision entrant + a combo through the whole loop.
	pubT, privT, _ := ed25519.GenerateKey(nil)
	var takerPK [32]byte
	copy(takerPK[:], pubT)
	taker := models.PubkeyString(takerPK)
	_ = privT
	st.Deposit(ctx, taker, 50_000_000)

	goalsPool := templates.MarketID(fixture, "precision_total_goals")
	if _, err := st.EnterPrecision(ctx, goalsPool, taker, 3, 2_000_000); err != nil {
		t.Fatalf("precision entry: %v", err)
	}

	// MM signs a quote on home_win(YES) + over_2_5(YES) — both settle YES at 2-1.
	pubM, privM, _ := ed25519.GenerateKey(nil)
	var makerPK [32]byte
	copy(makerPK[:], pubM)
	maker := models.PubkeyString(makerPK)
	st.Deposit(ctx, maker, 100_000_000)

	legs := []models.Leg{
		{MarketID: templates.MarketID(fixture, "home_win"), Outcome: models.OutcomeYes},
		{MarketID: templates.MarketID(fixture, "over_2_5"), Outcome: models.OutcomeYes},
	}
	rfqID, err := rfqSvc.CreateRFQ(ctx, taker, legs, 5_000_000)
	if err != nil {
		t.Fatalf("CreateRFQ: %v", err)
	}
	q := &models.ComboQuote{
		Maker: makerPK, Legs: legs, Stake: 5_000_000, Payout: 20_000_000,
		Expiry: time.Now().Add(time.Minute).Unix(), Salt: 1,
	}
	models.SignQuote(q, privM)
	if err := rfqSvc.SubmitQuote(ctx, q, rfqID); err != nil {
		t.Fatalf("SubmitQuote: %v", err)
	}
	if _, err := rfqSvc.Accept(ctx, models.QuoteHash(q), takerPK); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	// Stream the recorded match at high speed (15s of events in ~0.15s).
	provider := replay.New("../../fixtures", 1000)
	if err := svc.RunFeed(ctx, provider, fixture); err != nil {
		t.Fatalf("RunFeed: %v", err)
	}

	// Final 2-1: home_win YES, draw NO, away_win NO, over_2_5 YES, btts YES.
	want := map[string]string{
		"home_win": "yes", "draw": "no", "away_win": "no", "over_2_5": "yes", "btts": "yes",
	}
	for key, expect := range want {
		m, err := st.GetMarket(ctx, templates.MarketID(fixture, key))
		if err != nil {
			t.Fatalf("GetMarket %s: %v", key, err)
		}
		if m.Status != "settled" {
			t.Errorf("%s status = %s, want settled", key, m.Status)
		}
		var outcome struct {
			Result string `json:"result"`
		}
		json.Unmarshal(m.Outcome, &outcome)
		if outcome.Result != expect {
			t.Errorf("%s resolved %q, want %q", key, outcome.Result, expect)
		}
	}

	// Precision settled at actual=3; the (only) entrant guessed 3 → wins ~full pool.
	lb, err := st.PrecisionLeaderboard(ctx, goalsPool)
	if err != nil || len(lb) != 1 {
		t.Fatalf("leaderboard: %v %v", lb, err)
	}
	if lb[0].Score == nil || *lb[0].Score != 1 {
		t.Errorf("exact guess must score 1.0, got %v", lb[0].Score)
	}
	if lb[0].Payout == nil || *lb[0].Payout != 1_960_000 { // 2M − 2% rake
		t.Errorf("payout = %v, want 1960000", lb[0].Payout)
	}

	// Combo: both legs YES → won; taker pot = 20 USDC.
	escrows, err := st.EscrowsForWallet(ctx, taker)
	if err != nil || len(escrows) != 1 {
		t.Fatalf("escrows: %v %v", escrows, err)
	}
	if escrows[0].Status != "won" {
		t.Errorf("combo status = %s, want won", escrows[0].Status)
	}
	b, _ := st.GetBalance(ctx, taker)
	// 50 − 2 (precision) − 5 (stake) + 1.96 (pool win) + 20 (combo pot) = 64.96
	if b.Available != 64_960_000 {
		t.Errorf("taker final balance = %d, want 64960000", b.Available)
	}

	// Match state reached "finished".
	m, err := st.GetMatchByFixture(ctx, fixture)
	if err != nil || m.Status != "finished" {
		t.Errorf("match status = %s, want finished (%v)", m.Status, err)
	}
}

func TestMutexLegsRejected(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	rfqSvc := rfq.New(st, hub, nil, log)
	svc := lifecycle.New(st, hub, rfqSvc, nil, nil, log)

	const fixture = "mutex-test"
	if err := svc.RegisterFixture(ctx, fixture, "A", "B", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// home_win + draw are the same mutex group ("result") on the same match.
	legs := []models.Leg{
		{MarketID: templates.MarketID(fixture, "home_win"), Outcome: models.OutcomeYes},
		{MarketID: templates.MarketID(fixture, "draw"), Outcome: models.OutcomeYes},
	}
	if _, err := rfqSvc.CreateRFQ(ctx, "taker", legs, 1_000_000); err == nil {
		t.Fatal("mutex-conflicting legs must be rejected")
	}
	// home_win + over_2_5 are compatible.
	legs[1].MarketID = templates.MarketID(fixture, "over_2_5")
	if _, err := rfqSvc.CreateRFQ(ctx, "taker", legs, 1_000_000); err != nil {
		t.Fatalf("compatible legs rejected: %v", err)
	}
}

func TestAbandonedMatchVoidsEverything(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	rfqSvc := rfq.New(st, hub, nil, log)
	svc := lifecycle.New(st, hub, rfqSvc, nil, nil, log)

	const fixture = "abandoned"
	if err := svc.RegisterFixture(ctx, fixture, "A", "B", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, w := walletPair(1)
	st.Deposit(ctx, w, 10_000_000)
	pool := templates.MarketID(fixture, "precision_total_goals")
	if _, err := st.EnterPrecision(ctx, pool, w, 2, 3_000_000); err != nil {
		t.Fatal(err)
	}

	final := lifecycle.FinalScore{HomeGoals: 1, AwayGoals: 0, Abandoned: true}
	if err := svc.ResolveFixture(ctx, fixture, final); err != nil {
		t.Fatalf("ResolveFixture: %v", err)
	}

	b, _ := st.GetBalance(ctx, w)
	if b.Available != 10_000_000 {
		t.Errorf("VOID must refund the full stake: %+v", b)
	}
	m, _ := st.GetMarket(ctx, templates.MarketID(fixture, "home_win"))
	if m.Status != "void" {
		t.Errorf("binary market status = %s, want void", m.Status)
	}
}

func walletPair(b byte) ([32]byte, string) {
	var pk [32]byte
	pk[0], pk[31] = b, 0xFF
	return pk, models.PubkeyString(pk)
}
