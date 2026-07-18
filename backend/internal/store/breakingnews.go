package store

import (
	"context"
	"time"
)

// NewsInput is one hourly breaking-news row to persist. Every field traces to
// real data: headline/source/url/published from a real Exa article, yes_pct/
// delta from real odds. Never fabricated.
type NewsInput struct {
	MatchID     string
	MarketID    [32]byte
	Headline    string
	Summary     string // optional grounded one-sentence condense
	Source      string // source domain
	URL         string
	PublishedAt *time.Time
	YesPct      *int
	Delta       *int
}

// NewsRow is one breaking-news item served to the markets index panel.
type NewsRow struct {
	MatchID     string     `json:"match_id"`
	MarketID    string     `json:"market_id"` // 64-hex
	Home        string     `json:"home"`
	Away        string     `json:"away"`
	Question    string     `json:"question"` // representative market title
	Headline    string     `json:"headline"`
	Summary     string     `json:"summary,omitempty"`
	Source      string     `json:"source,omitempty"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	YesPct      *int       `json:"yes_pct,omitempty"`
	Delta       *int       `json:"delta,omitempty"`
	GeneratedAt time.Time  `json:"generated_at"`
}

// InsertBreakingNews persists one hourly news row.
func (s *Store) InsertBreakingNews(ctx context.Context, n NewsInput) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO breaking_news
		  (match_id, market_id, headline, summary, source, url, published_at, yes_pct, delta)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		n.MatchID, n.MarketID[:], n.Headline, n.Summary, n.Source, n.URL,
		n.PublishedAt, n.YesPct, n.Delta)
	return err
}

// PrevYesPct returns the market's last stored yes_pct (the baseline for the next
// momentum delta), or nil when there's no prior row.
func (s *Store) PrevYesPct(ctx context.Context, marketID [32]byte) *int {
	var v *int
	if err := s.pool.QueryRow(ctx, `
		SELECT yes_pct FROM breaking_news WHERE market_id = $1
		ORDER BY generated_at DESC LIMIT 1`, marketID[:]).Scan(&v); err != nil {
		return nil
	}
	return v
}

// LatestBreakingNews returns the freshest row per market within the window,
// ranked by pin (featured_rank) then momentum (|delta|). Powers GET /news.
func (s *Store) LatestBreakingNews(ctx context.Context, within time.Duration, limit int) ([]NewsRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	cutoff := time.Now().Add(-within)
	rows, err := s.pool.Query(ctx, `
		SELECT x.match_id, encode(x.market_id,'hex'), mt.home, mt.away, mk.title,
		       x.headline, COALESCE(x.summary,''), COALESCE(x.source,''), x.url,
		       x.published_at, x.yes_pct, x.delta, x.generated_at
		FROM (
		  SELECT DISTINCT ON (bn.market_id) bn.*
		  FROM breaking_news bn
		  WHERE bn.generated_at > $1
		  ORDER BY bn.market_id, bn.generated_at DESC
		) x
		JOIN markets mk ON mk.market_id = x.market_id
		JOIN matches mt ON mt.id = x.match_id
		ORDER BY mk.featured_rank NULLS LAST, abs(COALESCE(x.delta,0)) DESC, x.published_at DESC NULLS LAST
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NewsRow
	for rows.Next() {
		var n NewsRow
		if err := rows.Scan(&n.MatchID, &n.MarketID, &n.Home, &n.Away, &n.Question,
			&n.Headline, &n.Summary, &n.Source, &n.URL,
			&n.PublishedAt, &n.YesPct, &n.Delta, &n.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
