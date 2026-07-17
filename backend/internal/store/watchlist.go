package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// AddWatch favourites a market for a wallet. Idempotent (ON CONFLICT DO NOTHING);
// an unknown market_id trips the FK and surfaces as ErrNotFound.
func (s *Store) AddWatch(ctx context.Context, wallet string, marketID [32]byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO watchlists (wallet, market_id) VALUES ($1, $2)
		ON CONFLICT (wallet, market_id) DO NOTHING`, wallet, marketID[:])
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

// RemoveWatch unfavourites a market. Removing an absent entry is a no-op.
func (s *Store) RemoveWatch(ctx context.Context, wallet string, marketID [32]byte) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM watchlists WHERE wallet = $1 AND market_id = $2`, wallet, marketID[:])
	return err
}

// Watchlist returns a wallet's watched market ids, most-recently-added first.
func (s *Store) Watchlist(ctx context.Context, wallet string) ([][32]byte, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT market_id FROM watchlists WHERE wallet = $1
		ORDER BY created_at DESC`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][32]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var id [32]byte
		copy(id[:], b)
		out = append(out, id)
	}
	return out, rows.Err()
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
