package breakingnews

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

// OddsSource supplies a market's current implied Yes% (real odds), keyed by
// template. Satisfied by *txodds.Provider.OddsSnapshot. Optional — nil means
// news rows carry no % / momentum delta.
type OddsSource interface {
	OddsSnapshot(ctx context.Context, fixtureID string) (map[string]uint16, error)
}

// pricedTemplates are the binary templates TxLINE actually prices (so a Yes% is
// available). A pinned market wins regardless; otherwise we prefer one of these.
var pricedTemplates = map[string]bool{"dnb_home": true, "over_2_5": true, "ou_1h_075": true}

type Service struct {
	store  *store.Store
	search Searcher
	odds   OddsSource // optional
	sum    Summarizer // optional
	log    *slog.Logger

	Every      time.Duration // tick cadence
	Window     time.Duration // cover matches kicking off within this window
	SinceHours int           // article recency floor
}

func New(st *store.Store, search Searcher, odds OddsSource, sum Summarizer, log *slog.Logger) *Service {
	return &Service{
		store: st, search: search, odds: odds, sum: sum, log: log,
		Every: time.Hour, Window: 72 * time.Hour, SinceHours: 48,
	}
}

// GenerateOnce pulls one real recent article per relevant match and stores a
// breaking-news row tied to a representative market with a real Yes% + delta.
// A match with no relevant fresh article is skipped — never fabricated.
func (s *Service) GenerateOnce(ctx context.Context) error {
	matches, err := s.store.ListMatches(ctx)
	if err != nil {
		return err
	}
	year := time.Now().Year()
	for _, m := range matches {
		if !s.covered(m) {
			continue
		}
		markets, err := s.store.MarketsForMatch(ctx, m.ID)
		if err != nil {
			return err
		}
		rep, ok := representative(markets)
		if !ok {
			continue // no active market to attach news to
		}

		query := fmt.Sprintf("%s vs %s football %d team news preview lineup injuries", m.Home, m.Away, year)
		arts, err := s.search.Search(ctx, query, s.SinceHours)
		if err != nil {
			s.log.Warn("breakingnews: search", "match", m.Home+" v "+m.Away, "err", err)
			continue
		}
		art, ok := pickRelevant(arts, m.Home, m.Away)
		if !ok {
			s.log.Info("breakingnews: no relevant article", "match", m.Home+" v "+m.Away)
			continue // relevance guard — no fabrication
		}

		yesPct, delta := s.oddsSnapshot(ctx, m.FixtureID, rep)

		summary := art.Highlight
		if s.sum != nil {
			if cond, serr := s.sum.Summarize(ctx, m.Home, m.Away, art.Title, art.Highlight); serr == nil && cond != "" {
				summary = cond
			} else if serr != nil {
				s.log.Warn("breakingnews: summarize", "err", serr)
			}
		}

		if err := s.store.InsertBreakingNews(ctx, store.NewsInput{
			MatchID: m.ID, MarketID: rep.MarketID,
			Headline: art.Title, Summary: summary, Source: art.Source, URL: art.URL,
			PublishedAt: art.PublishedAt, YesPct: yesPct, Delta: delta,
		}); err != nil {
			return err
		}
		s.log.Info("breakingnews", "match", m.Home+" v "+m.Away, "source", art.Source, "headline", art.Title)
	}
	return nil
}

// oddsSnapshot resolves the representative market's current Yes% and the
// momentum delta versus the previous stored snapshot (both nil when odds aren't
// available for the market).
func (s *Service) oddsSnapshot(ctx context.Context, fixtureID string, rep store.MarketRow) (yes, delta *int) {
	if s.odds == nil {
		return nil, nil
	}
	snap, err := s.odds.OddsSnapshot(ctx, fixtureID)
	if err != nil {
		return nil, nil
	}
	cents, has := snap[rep.TemplateKey]
	if !has {
		return nil, nil
	}
	v := int(cents)
	yes = &v
	if prev := s.store.PrevYesPct(ctx, rep.MarketID); prev != nil {
		d := v - *prev
		delta = &d
	}
	return yes, delta
}

// covered reports whether a match is newsworthy now: live, kicking off within
// the window, or finished within the last 6h.
func (s *Service) covered(m store.MatchRow) bool {
	if m.Status == "live" {
		return true
	}
	until := time.Until(m.KickoffAt)
	if until > 0 {
		return until <= s.Window
	}
	return -until <= 6*time.Hour
}

// representative picks the market a match's news row attaches to: a pinned market
// (lowest featured_rank) wins; otherwise a TxLINE-priced binary; otherwise the
// first open/closed binary.
func representative(ms []store.MarketRow) (store.MarketRow, bool) {
	var best store.MarketRow
	found := false
	better := func(a, b store.MarketRow) bool {
		ra, rb := rankOf(a), rankOf(b)
		if ra != rb {
			return ra < rb
		}
		return pricedTemplates[a.TemplateKey] && !pricedTemplates[b.TemplateKey]
	}
	for _, m := range ms {
		if (m.Status != "open" && m.Status != "closed") || m.Type != "binary" {
			continue
		}
		if !found || better(m, best) {
			best, found = m, true
		}
	}
	return best, found
}

func rankOf(m store.MarketRow) int {
	if m.FeaturedRank == nil {
		return 1 << 30
	}
	return *m.FeaturedRank
}

// pickRelevant returns the first article that actually mentions a team — the
// guard that keeps off-topic search results out of the panel.
func pickRelevant(arts []Article, home, away string) (Article, bool) {
	h, a := strings.ToLower(home), strings.ToLower(away)
	for _, art := range arts {
		blob := strings.ToLower(art.Title + " " + art.Highlight)
		if (h != "" && strings.Contains(blob, h)) || (a != "" && strings.Contains(blob, a)) {
			return art, true
		}
	}
	return Article{}, false
}

// Run generates once at startup (so a fresh deploy has news immediately), then
// hourly until ctx cancels.
func (s *Service) Run(ctx context.Context) {
	if err := s.GenerateOnce(ctx); err != nil {
		s.log.Error("breakingnews: initial", "err", err)
	}
	t := time.NewTicker(s.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.GenerateOnce(ctx); err != nil {
				s.log.Error("breakingnews: tick", "err", err)
			}
		}
	}
}
