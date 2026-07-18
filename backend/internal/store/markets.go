package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	Lineups   []byte    `json:"lineups"`    // raw JSONB team sheets ('null' when unset)
}

// matchCols is the shared SELECT list for MatchRow (lineups is nullable; coalesce
// so scans never see SQL NULL). Prefix with a table alias via matchColsAliased.
const matchCols = `id, txodds_fixture_id, home, away, kickoff_at, status, live_state, COALESCE(lineups, 'null'::jsonb)`

func scanMatch(row pgx.Row) (MatchRow, error) {
	var m MatchRow
	err := row.Scan(&m.ID, &m.FixtureID, &m.Home, &m.Away, &m.KickoffAt, &m.Status, &m.LiveState, &m.Lineups)
	return m, err
}

type MarketRow struct {
	ID          string    `json:"id"`
	MarketID    [32]byte  `json:"-"`
	MatchID     string    `json:"match_id,omitempty"`
	TemplateKey string    `json:"template_key"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Rule        string    `json:"rule"`
	Status      string    `json:"status"`
	Outcome     []byte    `json:"outcome,omitempty"` // raw JSONB
	ChainTx     string    `json:"chain_tx,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	FeaturedRank *int      `json:"featured_rank,omitempty"` // nil = not pinned; lower = higher priority

	Scope            string `json:"scope"`
	CompetitionID    string `json:"competition_id,omitempty"`
	SubjectType      string `json:"subject_type,omitempty"`
	SubjectID        string `json:"subject_id,omitempty"`
	ResolutionSource string `json:"resolution_source"`
	RuleJSON         []byte `json:"rule_json,omitempty"`
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
	m, err := scanMatch(s.pool.QueryRow(ctx,
		`SELECT `+matchCols+` FROM matches WHERE txodds_fixture_id = $1`, fixtureID))
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// GetMatchByID fetches one match by its UUID id (the /matches/{id} detail route).
func (s *Store) GetMatchByID(ctx context.Context, id string) (MatchRow, error) {
	m, err := scanMatch(s.pool.QueryRow(ctx,
		`SELECT `+matchCols+` FROM matches WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMatches(ctx context.Context) ([]MatchRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+matchCols+` FROM matches ORDER BY kickoff_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchRow
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMatchLineups persists a fixture's team sheets (feed EventLineup). Static per
// match, so stored separately from the per-tick live_state.
func (s *Store) SetMatchLineups(ctx context.Context, fixtureID string, lineups []byte) error {
	ls := string(lineups)
	res, err := s.pool.Exec(ctx,
		`UPDATE matches SET lineups = $2::jsonb WHERE txodds_fixture_id = $1`, fixtureID, ls)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UnresolvedMatches returns matches kicked off before `olderThan` that still have
// at least one market awaiting resolution (status not settled/void). The
// resolution reconciler walks these to recover missed full-time events — a
// bounded, usually-empty set (only past-kickoff matches with open markets).
func (s *Store) UnresolvedMatches(ctx context.Context, olderThan time.Time) ([]MatchRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+matchCols+`
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
		m, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMarket inserts a market row in 'open' status. Idempotent on market_id.
func (s *Store) CreateMarket(ctx context.Context, marketID [32]byte, matchID, templateKey, typ, title, rule string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO markets (market_id, match_id, template_key, type, title, rule, status, scope, resolution_source)
		VALUES ($1,$2,$3,$4,$5,$6,'open','fixture','txline_fixture')
		ON CONFLICT (market_id) DO NOTHING`,
		marketID[:], matchID, templateKey, typ, title, rule)
	return err
}

const marketColumns = `id, market_id, COALESCE(match_id::text, ''), template_key, type, title, rule, status,
	COALESCE(outcome, 'null'::jsonb), COALESCE(chain_tx, ''), created_at,
	scope, COALESCE(competition_id, ''), COALESCE(subject_type, ''), COALESCE(subject_id, ''),
	resolution_source, rule_json, featured_rank`

func scanMarket(row pgx.Row) (MarketRow, error) {
	var m MarketRow
	var marketID []byte
	err := row.Scan(&m.ID, &marketID, &m.MatchID, &m.TemplateKey, &m.Type, &m.Title,
		&m.Rule, &m.Status, &m.Outcome, &m.ChainTx, &m.CreatedAt, &m.Scope,
		&m.CompetitionID, &m.SubjectType, &m.SubjectID, &m.ResolutionSource, &m.RuleJSON,
		&m.FeaturedRank)
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

// ListMarkets returns markets, optionally filtered by status ("" = all). Global
// (match-less) markets from the market builder are included: marketColumns
// COALESCEs match_id::text, so a NULL scans as "" rather than erroring the whole
// list (the prior hotfix that excluded them is superseded by real global-market
// support here — the frontend renders them by scope).
func (s *Store) ListMarkets(ctx context.Context, status string) ([]MarketRow, error) {
	q := `SELECT ` + marketColumns + ` FROM markets`
	args := []any{}
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY featured_rank NULLS LAST, created_at`
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

// SetMarketFeatured pins (rank != nil) or unpins (rank == nil) a market for the
// featured hero on the markets index. Lower rank = higher priority.
func (s *Store) SetMarketFeatured(ctx context.Context, marketID [32]byte, rank *int) error {
	res, err := s.pool.Exec(ctx,
		`UPDATE markets SET featured_rank = $2 WHERE market_id = $1`, marketID[:], rank)
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
	return s.tx(ctx, func(tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `
			UPDATE markets SET outcome = $2, chain_tx = $3, status = $4
			WHERE market_id = $1`, marketID[:], string(outcomeJSON), chainTx, status)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrNotFound
		}
		// A settled/void market can never match again — cancel every resting order
		// and return its soft-locked collateral to the maker (atomic with settle).
		// Winner payout is NOT done here: on-chain, winners claim via the redeem ix
		// (RedeemPosition on complete); in mirror mode the lifecycle calls
		// RedeemBinaryWinners. Crediting here would double-pay the on-chain claim.
		_, err = cancelOpenOrdersForMarket(ctx, tx, marketID[:])
		return err
	})
}

type CustomMarketRequest struct {
	Scope            string
	FixtureID        string
	MatchID          string
	TemplateKey      string
	Type             string
	Title            string
	Rule             string
	CompetitionID    string
	SubjectType      string
	SubjectID        string
	ResolutionSource string
	RuleJSON         json.RawMessage
}

func CustomMarketID(req CustomMarketRequest) [32]byte {
	scope := req.Scope
	if scope == "" {
		scope = "custom"
	}
	return sha256.Sum256([]byte("pitchmarket:v2:" + scope + ":" + req.FixtureID + ":" + req.CompetitionID + ":" +
		req.SubjectType + ":" + req.SubjectID + ":" + req.TemplateKey + ":" + req.Title))
}

func (s *Store) CreateCustomMarket(ctx context.Context, req CustomMarketRequest) ([32]byte, error) {
	if req.Scope == "" {
		req.Scope = "custom"
	}
	if req.Type == "" {
		req.Type = "binary"
	}
	if req.ResolutionSource == "" {
		req.ResolutionSource = "manual_required"
	}
	if len(req.RuleJSON) == 0 {
		req.RuleJSON = json.RawMessage(`{}`)
	}
	var matchID *string
	if req.MatchID != "" {
		matchID = &req.MatchID
	}
	marketID := CustomMarketID(req)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO markets (
			market_id, match_id, template_key, type, title, rule, status,
			scope, competition_id, subject_type, subject_id, resolution_source, rule_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'open', $7, $8, $9, $10, $11, $12::jsonb)
		ON CONFLICT (market_id) DO NOTHING`,
		marketID[:], matchID, req.TemplateKey, req.Type, req.Title, req.Rule, req.Scope,
		req.CompetitionID, req.SubjectType, req.SubjectID, req.ResolutionSource, string(req.RuleJSON))
	return marketID, err
}

type ResolutionAttempt struct {
	ID        string
	MarketID  [32]byte
	Actor     string
	Outcome   string
	Evidence  []byte
	Tx        string
	CreatedAt time.Time
}

func (s *Store) RecordResolutionAttempt(ctx context.Context, a ResolutionAttempt) error {
	if len(a.Evidence) == 0 {
		a.Evidence = []byte(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO resolution_attempts (market_id, actor, outcome, evidence, tx)
		VALUES ($1,$2,$3,$4::jsonb,$5)`,
		a.MarketID[:], a.Actor, a.Outcome, string(a.Evidence), a.Tx)
	return err
}

func (s *Store) ResolutionAttempts(ctx context.Context, marketID [32]byte) ([]ResolutionAttempt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, market_id, actor, outcome, evidence, COALESCE(tx, ''), created_at
		FROM resolution_attempts
		WHERE market_id = $1
		ORDER BY created_at DESC`, marketID[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResolutionAttempt
	for rows.Next() {
		var a ResolutionAttempt
		var rawID []byte
		if err := rows.Scan(&a.ID, &rawID, &a.Actor, &a.Outcome, &a.Evidence, &a.Tx, &a.CreatedAt); err != nil {
			return nil, err
		}
		copy(a.MarketID[:], rawID)
		out = append(out, a)
	}
	return out, rows.Err()
}
