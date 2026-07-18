package store_test

import (
	"testing"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
)

func TestWatchlistRoundTrip(t *testing.T) {
	s := storetest.Open(t)
	_, w := wallet(1)

	var m1, m2 [32]byte
	m1[0], m1[31] = 0xA1, 0x01
	m2[0], m2[31] = 0xB2, 0x02
	seedMarket(t, s, m1)
	seedMarket(t, s, m2)

	// Empty to start.
	got, err := s.Watchlist(ctx, w)
	if err != nil {
		t.Fatalf("Watchlist: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh watchlist = %d entries, want 0", len(got))
	}

	// Add two; most-recent-first ordering means m2 comes before m1.
	if err := s.AddWatch(ctx, w, m1); err != nil {
		t.Fatalf("AddWatch m1: %v", err)
	}
	if err := s.AddWatch(ctx, w, m2); err != nil {
		t.Fatalf("AddWatch m2: %v", err)
	}
	// Adding a duplicate is a no-op, not an error.
	if err := s.AddWatch(ctx, w, m1); err != nil {
		t.Fatalf("AddWatch duplicate: %v", err)
	}

	got, err = s.Watchlist(ctx, w)
	if err != nil {
		t.Fatalf("Watchlist: %v", err)
	}
	if len(got) != 2 || got[0] != m2 || got[1] != m1 {
		t.Fatalf("watchlist = %v, want [m2 m1]", got)
	}

	// Remove one.
	if err := s.RemoveWatch(ctx, w, m2); err != nil {
		t.Fatalf("RemoveWatch: %v", err)
	}
	got, _ = s.Watchlist(ctx, w)
	if len(got) != 1 || got[0] != m1 {
		t.Fatalf("after remove = %v, want [m1]", got)
	}
}

func TestWatchUnknownMarket(t *testing.T) {
	s := storetest.Open(t)
	_, w := wallet(2)
	var unknown [32]byte
	unknown[0], unknown[31] = 0xEE, 0xEE
	if err := s.AddWatch(ctx, w, unknown); err != store.ErrNotFound {
		t.Fatalf("AddWatch unknown market = %v, want ErrNotFound", err)
	}
}
