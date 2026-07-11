package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type PrecisionEntry struct {
	ID       string    `json:"id"`
	MarketID [32]byte  `json:"-"`
	Wallet   string    `json:"user"`
	Guess    float64   `json:"guess"`
	Stake    uint64    `json:"stake"`
	Score    *float64  `json:"score,omitempty"`
	Payout   *uint64   `json:"payout,omitempty"`
	TS       time.Time `json:"ts"`
}

// EnterPrecision stakes into a pool: kickoff-lock and market status are checked
// against the DB inside the transaction (ADR 0006 C2 — entry closes at kickoff,
// non-negotiable); one-entry-per-wallet is the unique constraint (C1 guard).
func (s *Store) EnterPrecision(ctx context.Context, marketID [32]byte, wallet string, guess float64, stake uint64) (string, error) {
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status string
		var kickoff time.Time
		err := tx.QueryRow(ctx, `
			SELECT m.status, ma.kickoff_at FROM markets m
			JOIN matches ma ON ma.id = m.match_id
			WHERE m.market_id = $1 AND m.type = 'precision'`, marketID[:]).
			Scan(&status, &kickoff)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "open" || !time.Now().Before(kickoff) {
			return ErrMarketNotOpen
		}

		res, err := tx.Exec(ctx, `
			UPDATE balances SET usdc_available = usdc_available - $2
			WHERE wallet = $1 AND usdc_available >= $2`, wallet, int64(stake))
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrInsufficientFunds
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO precision_entries (market_id, "user", guess, stake)
			VALUES ($1,$2,$3,$4) RETURNING id`,
			marketID[:], wallet, guess, int64(stake)).Scan(&id)
		if isUniqueViolation(err) {
			return ErrAlreadyEntered
		}
		return err
	})
	return id, err
}

// PrecisionLeaderboard lists entries, settled scores first (best score → best
// rank), unsettled by entry time.
func (s *Store) PrecisionLeaderboard(ctx context.Context, marketID [32]byte) ([]PrecisionEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, "user", guess, stake, score, payout, ts FROM precision_entries
		WHERE market_id = $1
		ORDER BY score DESC NULLS LAST, ts ASC`, marketID[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrecisionEntry
	for rows.Next() {
		var e PrecisionEntry
		var payout *int64
		if err := rows.Scan(&e.ID, &e.Wallet, &e.Guess, &e.Stake, &e.Score, &payout, &e.TS); err != nil {
			return nil, err
		}
		if payout != nil {
			p := uint64(*payout)
			e.Payout = &p
		}
		e.MarketID = marketID
		out = append(out, e)
	}
	return out, rows.Err()
}

// PrecisionScore is the σ-normalized closeness score (ADR 0006 C4):
// score = 1 / (1 + |guess − actual| / s)^k, with per-template scale s and k=2.
func PrecisionScore(guess, actual, scale float64, k float64) float64 {
	if scale <= 0 {
		scale = 1
	}
	return 1 / math.Pow(1+math.Abs(guess-actual)/scale, k)
}

// PrecisionSettlement summarizes a settled pool.
type PrecisionSettlement struct {
	Actual  float64 `json:"actual"`
	Pool    uint64  `json:"pool"`    // after rake, micro-USDC
	Rake    uint64  `json:"rake"`    // micro-USDC withheld
	Entries int     `json:"entries"`
}

// SettlePrecision scores every entry against the actual value and pays
// payout_i = Pool × (stake_i·score_i)/Σ(stake·score) (ADR 0006), crediting
// balances and marking the market settled. Void pools should instead refund via
// VoidPrecision.
func (s *Store) SettlePrecision(ctx context.Context, marketID [32]byte, actual, scale float64, k float64, rakeBps uint32, outcomeJSON []byte) (PrecisionSettlement, error) {
	var sum PrecisionSettlement
	err := s.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, "user", guess, stake FROM precision_entries
			WHERE market_id = $1 FOR UPDATE`, marketID[:])
		if err != nil {
			return err
		}
		type entry struct {
			id     string
			wallet string
			guess  float64
			stake  uint64
			score  float64
		}
		var entries []entry
		var total uint64
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.id, &e.wallet, &e.guess, &e.stake); err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, e)
			total += e.stake
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		rake := total * uint64(rakeBps) / 10_000
		pool := total - rake
		var weightSum float64
		for i := range entries {
			entries[i].score = PrecisionScore(entries[i].guess, actual, scale, k)
			weightSum += float64(entries[i].stake) * entries[i].score
		}

		for _, e := range entries {
			var payout uint64
			if weightSum > 0 {
				payout = uint64(float64(pool) * (float64(e.stake) * e.score) / weightSum)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE precision_entries SET score = $2, payout = $3 WHERE id = $1`,
				e.id, e.score, int64(payout)); err != nil {
				return err
			}
			if payout > 0 {
				if _, err := tx.Exec(ctx, `
					INSERT INTO balances (wallet, usdc_available) VALUES ($1,$2)
					ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2`,
					e.wallet, int64(payout)); err != nil {
					return err
				}
			}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE markets SET status = 'settled', outcome = $2 WHERE market_id = $1`,
			marketID[:], string(outcomeJSON)); err != nil {
			return err
		}
		sum = PrecisionSettlement{Actual: actual, Pool: pool, Rake: rake, Entries: len(entries)}
		return nil
	})
	return sum, err
}

// VoidPrecision refunds every stake (abandoned match → pool VOID, ADR 0006).
func (s *Store) VoidPrecision(ctx context.Context, marketID [32]byte) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT "user", stake FROM precision_entries WHERE market_id = $1 FOR UPDATE`,
			marketID[:])
		if err != nil {
			return err
		}
		type refund struct {
			wallet string
			stake  int64
		}
		var refunds []refund
		for rows.Next() {
			var r refund
			if err := rows.Scan(&r.wallet, &r.stake); err != nil {
				rows.Close()
				return err
			}
			refunds = append(refunds, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range refunds {
			if _, err := tx.Exec(ctx, `
				UPDATE balances SET usdc_available = usdc_available + $2 WHERE wallet = $1`,
				r.wallet, r.stake); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE precision_entries SET payout = $2, score = 0
				WHERE market_id = $1 AND "user" = $3`,
				marketID[:], r.stake, r.wallet); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx,
			`UPDATE markets SET status = 'void' WHERE market_id = $1`, marketID[:])
		return err
	})
}
