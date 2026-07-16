package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type MatchRow struct {
	ID        string    `json:"id"`
	FixtureID string    `json:"txodds_fixture_id"`
	Home      string    `json:"home"`
	Away      string    `json:"away"`
	KickoffAt time.Time `json:"kickoff_at"`
	Status    string    `json:"status"`
	LiveState []byte    `json:"live_state"` // raw JSONB
}

type MarketRow struct {
	ID          string    `json:"id"`
	MarketID    [32]byte  `json:"-"`
	MatchID     string    `json:"match_id"`
	TemplateKey string    `json:"template_key"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Rule        string    `json:"rule"`
	Status      string    `json:"status"`
	Outcome     []byte    `json:"outcome,omitempty"` // raw JSONB
	ChainTx     string    `json:"chain_tx,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// UpsertMatch registers a fixture (feed-driven auto market creation, PROJECT_PLAN §3).
func (s *Store) UpsertMatch(ctx context.Context, fixtureID, home, away string, kickoff time.Time) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO matches (txodds_fixture_id, home, away, kickoff_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (txodds_fixture_id) DO UPDATE SET home = $2, away = $3, kickoff_at = $4
		RETURNING id`, fixtureID, home, away, kickoff).Scan(&id)
	return id, err
}

// SetMatchState updates status + live_state from feed events (match_state WS source).
func (s *Store) SetMatchState(ctx context.Context, fixtureID, status string, liveState []byte) error {
	var ls *string // JSONB params must travel as text, nil keeps the old value
	if liveState != nil {
		v := string(liveState)
		ls = &v
	}
	res, err := s.pool.Exec(ctx, `
		UPDATE matches SET status = $2, live_state = COALESCE($3::jsonb, live_state)
		WHERE txodds_fixture_id = $1`, fixtureID, status, ls)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetMatchByFixture(ctx context.Context, fixtureID string) (MatchRow, error) {
	var m MatchRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, txodds_fixture_id, home, away, kickoff_at, status, live_state
		FROM matches WHERE txodds_fixture_id = $1`, fixtureID).
		Scan(&m.ID, &m.FixtureID, &m.Home, &m.Away, &m.KickoffAt, &m.Status, &m.LiveState)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMatches(ctx context.Context) ([]MatchRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, txodds_fixture_id, home, away, kickoff_at, status, live_state
		FROM matches ORDER BY kickoff_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchRow
	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(&m.ID, &m.FixtureID, &m.Home, &m.Away, &m.KickoffAt, &m.Status, &m.LiveState); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnresolvedMatches returns matches kicked off before `olderThan` that still have
// at least one market awaiting resolution (status not settled/void). The
// resolution reconciler walks these to recover missed full-time events — a
// bounded, usually-empty set (only past-kickoff matches with open markets).
func (s *Store) UnresolvedMatches(ctx context.Context, olderThan time.Time) ([]MatchRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.txodds_fixture_id, m.home, m.away, m.kickoff_at, m.status, m.live_state
		FROM matches m
		WHERE m.kickoff_at < $1
		  AND EXISTS (SELECT 1 FROM markets mk
		              WHERE mk.match_id = m.id AND mk.status NOT IN ('settled','void'))
		ORDER BY m.kickoff_at`, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchRow
	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(&m.ID, &m.FixtureID, &m.Home, &m.Away, &m.KickoffAt, &m.Status, &m.LiveState); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMarket inserts a market row in 'open' status. Idempotent on market_id.
func (s *Store) CreateMarket(ctx context.Context, marketID [32]byte, matchID, templateKey, typ, title, rule string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO markets (market_id, match_id, template_key, type, title, rule, status)
		VALUES ($1,$2,$3,$4,$5,$6,'open')
		ON CONFLICT (market_id) DO NOTHING`,
		marketID[:], matchID, templateKey, typ, title, rule)
	return err
}

const marketColumns = `id, market_id, match_id, template_key, type, title, rule, status,
	COALESCE(outcome, 'null'::jsonb), COALESCE(chain_tx, ''), created_at`

func scanMarket(row pgx.Row) (MarketRow, error) {
	var m MarketRow
	var marketID []byte
	err := row.Scan(&m.ID, &marketID, &m.MatchID, &m.TemplateKey, &m.Type, &m.Title,
		&m.Rule, &m.Status, &m.Outcome, &m.ChainTx, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	copy(m.MarketID[:], marketID)
	return m, err
}

func (s *Store) GetMarket(ctx context.Context, marketID [32]byte) (MarketRow, error) {
	return scanMarket(s.pool.QueryRow(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE market_id = $1`, marketID[:]))
}

// ListMarkets returns markets, optionally filtered by status ("" = all).
func (s *Store) ListMarkets(ctx context.Context, status string) ([]MarketRow, error) {
	q := `SELECT ` + marketColumns + ` FROM markets`
	args := []any{}
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketRow
	for rows.Next() {
		m, err := scanMarket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarketsForMatch lists a match's markets (the /match/[id] page).
func (s *Store) MarketsForMatch(ctx context.Context, matchID string) ([]MarketRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+marketColumns+` FROM markets WHERE match_id = $1 ORDER BY created_at`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketRow
	for rows.Next() {
		m, err := scanMarket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SetMarketStatus(ctx context.Context, marketID [32]byte, status string) error {
	res, err := s.pool.Exec(ctx,
		`UPDATE markets SET status = $2 WHERE market_id = $1`, marketID[:], status)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SettleMarket records the resolved outcome + the on-chain resolve_market tx
// (the /settlement/[id] "Verified on Solana" link).
func (s *Store) SettleMarket(ctx context.Context, marketID [32]byte, outcomeJSON []byte, chainTx, status string) error {
	res, err := s.pool.Exec(ctx, `
		UPDATE markets SET outcome = $2, chain_tx = $3, status = $4
		WHERE market_id = $1`, marketID[:], string(outcomeJSON), chainTx, status)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
