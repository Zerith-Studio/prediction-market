package store

import (
	"context"
	"encoding/json"
	"time"
)

// InsertOneliners stores a generation batch (6 lines/market, PROJECT_PLAN §3).
func (s *Store) InsertOneliners(ctx context.Context, marketID [32]byte, lines []string) error {
	raw, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO oneliners (market_id, lines) VALUES ($1, $2)`, marketID[:], string(raw))
	return err
}

// LatestOneliners returns the most recent batch for a market (GET /markets/:id/oneliners).
func (s *Store) LatestOneliners(ctx context.Context, marketID [32]byte) ([]string, time.Time, error) {
	var raw []byte
	var at time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT lines, generated_at FROM oneliners
		WHERE market_id = $1 ORDER BY generated_at DESC LIMIT 1`, marketID[:]).
		Scan(&raw, &at)
	if err != nil {
		return nil, at, ErrNotFound
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err != nil {
		return nil, at, err
	}
	return lines, at, nil
}

// FillView is one row of fill history for API/portfolio reads.
type FillView struct {
	ID        string    `json:"id"`
	MarketID  string    `json:"market_id"`
	TakerHash string    `json:"taker_hash"`
	MakerHash string    `json:"maker_hash"`
	Price     uint16    `json:"price"`
	Size      uint64    `json:"size"`
	MatchType string    `json:"match_type"`
	SettleTx  string    `json:"settle_tx,omitempty"`
	TS        time.Time `json:"ts"`
}

// FillsForMarket lists recent fills (GET /markets/:id trade history).
func (s *Store) FillsForMarket(ctx context.Context, marketID [32]byte, limit int) ([]FillView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, encode(market_id,'hex'), encode(taker_hash,'hex'), encode(maker_hash,'hex'),
		       price, size, match_type, COALESCE(settle_tx,''), ts
		FROM fills WHERE market_id = $1 ORDER BY ts DESC LIMIT $2`, marketID[:], limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFills(rows)
}

// FillsForWallet lists fills touching any of the wallet's orders (portfolio).
func (s *Store) FillsForWallet(ctx context.Context, wallet string, limit int) ([]FillView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, encode(f.market_id,'hex'), encode(f.taker_hash,'hex'), encode(f.maker_hash,'hex'),
		       f.price, f.size, f.match_type, COALESCE(f.settle_tx,''), f.ts
		FROM fills f
		WHERE f.taker_hash IN (SELECT order_hash FROM orders WHERE maker = $1)
		   OR f.maker_hash IN (SELECT order_hash FROM orders WHERE maker = $1)
		ORDER BY f.ts DESC LIMIT $2`, wallet, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFills(rows)
}

func scanFills(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]FillView, error) {
	var out []FillView
	for rows.Next() {
		var f FillView
		if err := rows.Scan(&f.ID, &f.MarketID, &f.TakerHash, &f.MakerHash,
			&f.Price, &f.Size, &f.MatchType, &f.SettleTx, &f.TS); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
