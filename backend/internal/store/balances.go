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

// redeemBinaryWinners pays out a resolved binary market off-chain, atomically with
// settlement (called inside SettleMarket's tx): winning-outcome holders are
// credited $1 (1e6 micro) per share, losing shares are worthless, and a VOID
// refunds each holder what they paid (avg_cost). All positions on the market are
// then cleared. On-chain, a winner redeems winning shares 1:1 via the `redeem` ix;
// this is the off-chain mirror credit — the same auto-payout precision/combos use.
func redeemBinaryWinners(ctx context.Context, tx pgx.Tx, marketID []byte, result string) error {
	if result != "yes" && result != "no" && result != "void" {
		return nil // unknown/unset outcome — nothing to redeem
	}
	rows, err := tx.Query(ctx, `
		SELECT "user", yes, no, avg_cost FROM positions_cache
		WHERE market_id = $1 AND (yes > 0 OR no > 0)`, marketID)
	if err != nil {
		return err
	}
	type holder struct {
		wallet  string
		yes, no int64
		avgCost int64
	}
	// Collect first — the balance writes below can't run while the cursor is open.
	var holders []holder
	for rows.Next() {
		var h holder
		if err := rows.Scan(&h.wallet, &h.yes, &h.no, &h.avgCost); err != nil {
			rows.Close()
			return err
		}
		holders = append(holders, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	const microPerShare = 1_000_000 // a winning share redeems for $1
	for _, h := range holders {
		var payout int64
		switch result {
		case "yes":
			payout = h.yes * microPerShare
		case "no":
			payout = h.no * microPerShare
		case "void":
			payout = (h.yes + h.no) * h.avgCost * 10_000 // refund cost (cents×shares→micro)
		}
		if payout <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO balances (wallet, usdc_available) VALUES ($1,$2)
			ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2`,
			h.wallet, payout); err != nil {
			return err
		}
	}
	// Positions are settled: winning shares redeemed to USDC, losing shares void.
	_, err = tx.Exec(ctx, `
		UPDATE positions_cache SET yes = 0, no = 0, yes_locked = 0, no_locked = 0
		WHERE market_id = $1`, marketID)
	return err
}

// RedeemBinaryWinners auto-pays every winner of a resolved binary market off-chain
// — mirror mode only. On-chain, winners claim via the redeem ix (RedeemPosition),
// so this must not run there or it would double-pay.
func (s *Store) RedeemBinaryWinners(ctx context.Context, marketID [32]byte, result string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		return redeemBinaryWinners(ctx, tx, marketID[:], result)
	})
}

// RedeemPosition mirrors an on-chain redeem for ONE holder: credits their winning
// shares at $1 each (VOID refunds avg_cost) and clears their position on the
// market. Returns the amount credited (micro-USDC). Called by
// /wallet/redeem-complete after the on-chain redeem confirms.
func (s *Store) RedeemPosition(ctx context.Context, wallet string, marketID [32]byte, result string) (uint64, error) {
	var credited int64
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var yes, no, avgCost int64
		e := tx.QueryRow(ctx, `
			SELECT yes, no, avg_cost FROM positions_cache
			WHERE "user" = $1 AND market_id = $2 FOR UPDATE`, wallet, marketID[:]).Scan(&yes, &no, &avgCost)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil // nothing held — no-op
		}
		if e != nil {
			return e
		}
		switch result {
		case "yes":
			credited = yes * 1_000_000
		case "no":
			credited = no * 1_000_000
		case "void":
			credited = (yes + no) * avgCost * 10_000
		}
		if credited > 0 {
			if _, e := tx.Exec(ctx, `
				INSERT INTO balances (wallet, usdc_available) VALUES ($1,$2)
				ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2`,
				wallet, credited); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `
			UPDATE positions_cache SET yes = 0, no = 0, yes_locked = 0, no_locked = 0
			WHERE "user" = $1 AND market_id = $2`, wallet, marketID[:])
		return e
	})
	if err != nil {
		return 0, err
	}
	return uint64(credited), nil
}

// PositionFor returns a wallet's held yes/no shares on one market (0 if none).
func (s *Store) PositionFor(ctx context.Context, wallet string, marketID [32]byte) (yes, no uint64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(yes,0), COALESCE(no,0) FROM positions_cache
		WHERE "user" = $1 AND market_id = $2`, wallet, marketID[:]).Scan(&yes, &no)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	return yes, no, err
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
		INSERT INTO users (privy_id, wallet, avatar_seed) VALUES ($1, $2, $2)
		ON CONFLICT (privy_id) DO NOTHING`, privyID, wallet)
	return err
}
