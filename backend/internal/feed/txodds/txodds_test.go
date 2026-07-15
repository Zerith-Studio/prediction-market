package txodds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
)

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New("", ""); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if _, err := New("https://x", ""); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestStreamParsesSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keep-alive comment\n")
		fmt.Fprint(w, "event: score\n")
		fmt.Fprint(w, `data: {"type":"score","payload":{"home_goals":1,"away_goals":0}}`+"\n\n")
		fmt.Fprint(w, "data: not-json garbage frame\n\n")
		fmt.Fprint(w, `data: {"fixture_id":"f-9","type":"full_time","payload":{"home_goals":1,"away_goals":0}}`+"\n\n")
	}))
	defer srv.Close()

	p, err := New(srv.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := p.Stream(ctx, "f-9")
	if err != nil {
		t.Fatal(err)
	}

	var got []feed.MatchEvent
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 parsed events (garbage skipped), got %d: %+v", len(got), got)
	}
	if got[0].Type != feed.EventScore || got[0].FixtureID != "f-9" {
		t.Errorf("first event: %+v (fixture id must default from the subscription)", got[0])
	}
	if got[1].Type != feed.EventFullTime {
		t.Errorf("second event: %+v", got[1])
	}
}

func TestStreamRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	p, _ := New(srv.URL, "k")
	if _, err := p.Stream(context.Background(), "f"); err == nil {
		t.Fatal("non-200 stream must error")
	}
}
