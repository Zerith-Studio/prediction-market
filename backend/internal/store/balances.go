package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Balance struct {
	Wallet    string `json:"wallet"`
	Available uint64 `json:"usdc_available"` // micro-USDC
	Locked    uint64 `json:"usdc_locked"`
}

type Position struct {
	Wallet    string   `json:"-"`
	MarketID  [32]byte `json:"-"`
	Yes       uint64   `json:"yes"`
	No        uint64   `json:"no"`
	YesLocked uint64   `json:"yes_locked"`
	NoLocked  uint64   `json:"no_locked"`
	AvgCost   uint64   `json:"avg_cost"` // running average, price-cents
	Realized  int64    `json:"realized"` // micro-USDC, Σ (exec − avg_cost)·size on sells
}

// Deposit mirrors an on-chain vault deposit into the demo ledger
// (POST /wallet/deposit). Chain is authoritative; the index package reconciles.
func (s *Store) Deposit(ctx context.Context, wallet string, amount uint64) (Balance, error) {
	var b Balance
	err := s.pool.QueryRow(ctx, `
		INSERT INTO balances (wallet, usdc_available) VALUES ($1, $2)
		ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2
		RETURNING wallet, usdc_available, usdc_locked`,
		wallet, int64(amount)).Scan(&b.Wallet, &b.Available, &b.Locked)
	return b, err
}

func (s *Store) GetBalance(ctx context.Context, wallet string) (Balance, error) {
	var b Balance
	err := s.pool.QueryRow(ctx, `
		SELECT wallet, usdc_available, usdc_locked FROM balances WHERE wallet = $1`,
		wallet).Scan(&b.Wallet, &b.Available, &b.Locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Balance{Wallet: wallet}, nil // zero balance, not an error
	}
	return b, err
}

// GrantTokens credits outcome tokens outside of trading (test/demo seeding of
// SELL-side inventory; on-chain equivalent is a prior MINT fill).
func (s *Store) GrantTokens(ctx context.Context, wallet string, marketID [32]byte, yes, no uint64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO positions_cache ("user", market_id, yes, no) VALUES ($1,$2,$3,$4)
		ON CONFLICT ("user", market_id) DO UPDATE
		SET yes = positions_cache.yes + $3, no = positions_cache.no + $4`,
		wallet, marketID[:], int64(yes), int64(no))
	return err
}

func (s *Store) GetPositions(ctx context.Context, wallet string) ([]Position, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT "user", market_id, yes, no, yes_locked, no_locked, avg_cost, realized
		FROM positions_cache WHERE "user" = $1`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Position
	for rows.Next() {
		var p Position
		var marketID []byte
		if err := rows.Scan(&p.Wallet, &marketID, &p.Yes, &p.No, &p.YesLocked, &p.NoLocked, &p.AvgCost, &p.Realized); err != nil {
			return nil, err
		}
		copy(p.MarketID[:], marketID)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertUser registers a Privy login → wallet binding.
func (s *Store) UpsertUser(ctx context.Context, privyID, wallet string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (privy_id, wallet) VALUES ($1, $2)
		ON CONFLICT (privy_id) DO NOTHING`, privyID, wallet)
	return err
}
