package lifecycle_test

// Resolution durability: ResolveFixture must be safely re-runnable (the
// reconciler re-invokes it), and UnresolvedMatches must surface a stuck match
// and drop it once resolved. Together these back the "recover a missed
// full-time event on restart" guarantee.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

func TestResolveFixtureIdempotent(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	svc := lifecycle.New(st, hub, rfq.New(st, hub, nil, log), nil, nil, log)

	const fixture = "reconcile-idem"
	// Kickoff in the future so the precision entry clears the kickoff-lock; the
	// idempotency of ResolveFixture is independent of the match clock.
	if err := svc.RegisterFixture(ctx, fixture, "A", "B", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, w := walletPair(7)
	st.Deposit(ctx, w, 10_000_000)
	pool := templates.MarketID(fixture, "precision_total_goals")
	if _, err := st.EnterPrecision(ctx, pool, w, 3, 2_000_000); err != nil {
		t.Fatal(err)
	}
	final := lifecycle.FinalScore{HomeGoals: 2, AwayGoals: 1, HTHomeGoals: 1, HTAwayGoals: 0}

	if err := svc.ResolveFixture(ctx, fixture, final); err != nil {
		t.Fatalf("resolve #1: %v", err)
	}
	after1, _ := st.GetBalance(ctx, w)

	// The reconciler re-runs ResolveFixture: it must be a safe no-op — no error,
	// and critically no double payout of the precision pool.
	if err := svc.ResolveFixture(ctx, fixture, final); err != nil {
		t.Fatalf("resolve #2 (idempotent) errored: %v", err)
	}
	after2, _ := st.GetBalance(ctx, w)
	if after1.Available != after2.Available {
		t.Errorf("re-resolve double-moved money: %d → %d", after1.Available, after2.Available)
	}
	if m, _ := st.GetMarket(ctx, templates.MarketID(fixture, "home_win")); m.Status != "settled" {
		t.Errorf("home_win status = %s, want settled", m.Status)
	}
}

func TestUnresolvedMatchesRecovery(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	svc := lifecycle.New(st, hub, rfq.New(st, hub, nil, log), nil, nil, log)

	const fixture = "reconcile-unresolved"
	// Kicked off in the past with all markets open → a stuck match the reconciler
	// would pick up.
	if err := svc.RegisterFixture(ctx, fixture, "A", "B", time.Now().Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	un, err := st.UnresolvedMatches(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !containsFixture(un, fixture) {
		t.Fatalf("UnresolvedMatches should include the past-kickoff open match")
	}

	// Once resolved (passes supplied so the passes pool settles too), it drops out.
	passes := 500
	final := lifecycle.FinalScore{HomeGoals: 1, AwayGoals: 0, TotalPasses: &passes}
	if err := svc.ResolveFixture(ctx, fixture, final); err != nil {
		t.Fatal(err)
	}
	un2, _ := st.UnresolvedMatches(ctx, time.Now())
	if containsFixture(un2, fixture) {
		t.Errorf("a fully-resolved match must no longer be unresolved")
	}
}

func containsFixture(rows []store.MatchRow, fixtureID string) bool {
	for _, m := range rows {
		if m.FixtureID == fixtureID {
			return true
		}
	}
	return false
}
