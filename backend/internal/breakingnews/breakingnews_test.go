package breakingnews

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

func TestExaSearchParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Spain XI vs Argentina: Confirmed team news","url":"https://ca.sports.yahoo.com/news/spain-xi.html","publishedDate":"2026-07-17T18:56:16.000Z","highlights":["Spain confirm their XI with Yamal starting up front."],"text":"full text"},
			{"title":"No link result","url":"","highlights":["dropped: no url"]}
		]}`))
	}))
	defer srv.Close()

	e := &Exa{APIKey: "k", Base: srv.URL, Client: srv.Client()}
	arts, err := e.Search(context.Background(), "Spain vs Argentina", 48)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(arts) != 1 { // the URL-less result is dropped
		t.Fatalf("want 1 article, got %d", len(arts))
	}
	a := arts[0]
	if a.Title != "Spain XI vs Argentina: Confirmed team news" {
		t.Errorf("title: %q", a.Title)
	}
	if a.Source != "ca.sports.yahoo.com" {
		t.Errorf("source (domain): %q", a.Source)
	}
	if a.Highlight == "" {
		t.Error("highlight should be the real excerpt")
	}
	if a.PublishedAt == nil || a.PublishedAt.Year() != 2026 {
		t.Errorf("publishedAt not parsed: %v", a.PublishedAt)
	}
}

func TestPickRelevantGuard(t *testing.T) {
	arts := []Article{
		{Title: "Wall Street closes higher on tech rally", Highlight: "Nasdaq up 2%."},
		{Title: "Argentina squad announced for the final", Highlight: "Messi to captain."},
	}
	got, ok := pickRelevant(arts, "Spain", "Argentina")
	if !ok || got.Title != "Argentina squad announced for the final" {
		t.Fatalf("relevance guard should pick the Argentina article, got %+v ok=%v", got, ok)
	}
	// Nothing mentioning either team → no row (no fabrication).
	if _, ok := pickRelevant([]Article{{Title: "Tesla stock jumps", Highlight: "EV demand"}}, "Spain", "Argentina"); ok {
		t.Error("off-topic results must be rejected")
	}
}

func TestRepresentativePrefersPinnedThenPriced(t *testing.T) {
	rank := func(n int) *int { return &n }
	pinned := store.MarketRow{Type: "binary", Status: "open", TemplateKey: "some_key", FeaturedRank: rank(1)}
	priced := store.MarketRow{Type: "binary", Status: "open", TemplateKey: "dnb_home"}
	other := store.MarketRow{Type: "binary", Status: "open", TemplateKey: "misc"}
	precision := store.MarketRow{Type: "precision", Status: "open", TemplateKey: "precision_total_goals"}
	settled := store.MarketRow{Type: "binary", Status: "settled", TemplateKey: "dnb_home"}

	// pinned beats a priced binary
	if rep, ok := representative([]store.MarketRow{other, priced, pinned}); !ok || rep.FeaturedRank == nil {
		t.Fatalf("pinned market should win: %+v ok=%v", rep, ok)
	}
	// with no pin, the TxLINE-priced binary wins over a generic one
	if rep, ok := representative([]store.MarketRow{other, priced}); !ok || rep.TemplateKey != "dnb_home" {
		t.Fatalf("priced binary should win: %+v", rep)
	}
	// precision + settled are ignored; only the active binary qualifies
	if rep, ok := representative([]store.MarketRow{precision, settled, other}); !ok || rep.TemplateKey != "misc" {
		t.Fatalf("should fall back to the one active binary: %+v ok=%v", rep, ok)
	}
	// nothing active → no representative
	if _, ok := representative([]store.MarketRow{precision, settled}); ok {
		t.Error("no active binary should yield no representative")
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"https://www.goal.com/en-us/news/x": "goal.com",
		"https://ca.sports.yahoo.com/news":  "ca.sports.yahoo.com",
		"not a url":                         "not a url", // url.Parse tolerates it; hostname empty → falls through
	}
	for in, want := range cases {
		if got := domainOf(in); got != want && !(want == "not a url" && got == "") {
			t.Errorf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}
