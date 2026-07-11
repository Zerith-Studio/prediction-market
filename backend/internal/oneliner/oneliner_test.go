package oneliner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

type fakeGen struct{ calls int }

func (f *fakeGen) Lines(context.Context, MatchContext) ([]string, error) {
	f.calls++
	return []string{"l1", "l2", "l3", "l4", "l5", "l6"}, nil
}

func TestGenerateOnceOnlyLiveMatches(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	log := slog.Default()
	hub := ws.NewHub(log)
	gen := &fakeGen{}
	svc := New(st, hub, gen, log)

	// A scheduled (not live) match → no generations.
	matchID, err := st.UpsertMatch(ctx, "one-1", "A", "B", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	marketID := templates.MarketID("one-1", "home_win")
	if err := st.CreateMarket(ctx, marketID, matchID, "home_win", "binary", "A to win", "rule"); err != nil {
		t.Fatal(err)
	}
	if err := svc.GenerateOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("scheduled match must not generate, got %d calls", gen.calls)
	}

	// Flip live → six lines stored per open market.
	if err := st.SetMatchState(ctx, "one-1", "live", []byte(`{"minute":10}`)); err != nil {
		t.Fatal(err)
	}
	if err := svc.GenerateOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("want 1 generation, got %d", gen.calls)
	}
	lines, _, err := st.LatestOneliners(ctx, marketID)
	if err != nil || len(lines) != 6 {
		t.Fatalf("stored lines: %v %v", lines, err)
	}
}

func TestExtractLines(t *testing.T) {
	lines, err := extractLines("Here you go:\n```json\n[\"a\",\"b\",\"c\",\"d\",\"e\",\"f\"]\n```")
	if err != nil || len(lines) != 6 || lines[0] != "a" {
		t.Fatalf("extractLines: %v %v", lines, err)
	}
	if _, err := extractLines("no json array here"); err == nil {
		t.Fatal("garbage must error")
	}
}
