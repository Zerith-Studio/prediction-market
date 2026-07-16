package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// OrderRow is the Postgres mirror of a resting order (chain OrderStatus is the
// authoritative fill-accounting; `Remaining` here is a UX mirror).
type OrderRow struct {
	OrderHash [32]byte
	Order     models.Order
	Remaining uint64
	Locked    uint64
	Status    string
	Seq       int64
}

// PlaceOrder soft-locks entry collateral (ADR 0002: BUY locks price×size+fee
// USDC, SELL locks `size` outcome tokens) and inserts the order row. It is the
// E2-entry enforcement of interface-contract.md §1; the chain re-checks on
// settle regardless.
func (s *Store) PlaceOrder(ctx context.Context, o *models.Order) error {
	if o.Size == 0 || o.Size > models.MaxOrderSize {
		return ErrBadOrderSize
	}
	hash := models.OrderHash(o)
	wallet := models.PubkeyString(o.Maker)

	return s.tx(ctx, func(tx pgx.Tx) error {
		var lock uint64
		if o.Side == models.SideBuy {
			lock = models.BuyCost(o.Price, o.Size, o.FeeBps)
		} else {
			lock = o.Size
		}

		// Insert first so a replayed hash fails as ErrDuplicateOrder before any
		// balance effect (the tx rolls the row back on later errors anyway).
		var expiry *time.Time
		if o.Expiry != 0 {
			t := time.Unix(o.Expiry, 0).UTC()
			expiry = &t
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO orders (order_hash, market_id, maker, outcome, side, price, size,
			                    remaining, fee_bps, expiry, salt, sig, locked, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10,$11,$12,'live')`,
			hash[:], o.MarketID[:], wallet, int16(o.Outcome), int16(o.Side), int16(o.Price),
			int64(o.Size), int16(o.FeeBps), expiry, int64(o.Salt), o.Sig[:], int64(lock))
		if isUniqueViolation(err) {
			return ErrDuplicateOrder
		}
		if err != nil {
			return err
		}

		if o.Side == models.SideBuy {
			res, err := tx.Exec(ctx, `
				UPDATE balances
				SET usdc_available = usdc_available - $2, usdc_locked = usdc_locked + $2
				WHERE wallet = $1 AND usdc_available >= $2`,
				wallet, int64(lock))
			if err != nil {
				return fmt.Errorf("store: lock USDC: %w", err)
			}
			if res.RowsAffected() == 0 {
				return ErrInsufficientFunds
			}
		} else {
			col := "yes"
			if o.Outcome == models.OutcomeNo {
				col = "no"
			}
			// SELL for tokens you don't hold is rejected at entry — no naked shorts.
			res, err := tx.Exec(ctx, fmt.Sprintf(`
				UPDATE positions_cache
				SET %[1]s_locked = %[1]s_locked + $3
				WHERE "user" = $1 AND market_id = $2 AND %[1]s - %[1]s_locked >= $3`, col),
				wallet, o.MarketID[:], int64(o.Size))
			if err != nil {
				return fmt.Errorf("store: lock tokens: %w", err)
			}
			if res.RowsAffected() == 0 {
				return ErrInsufficientTokens
			}
		}
		return nil
	})
}

// CancelOrder marks the order cancelled and releases its residual soft-lock.
// Only the order's maker may cancel (checked against the stored row).
func (s *Store) CancelOrder(ctx context.Context, hash [32]byte, maker [32]byte) error {
	wallet := models.PubkeyString(maker)
	return s.tx(ctx, func(tx pgx.Tx) error {
		var side, outcome int16
		var locked int64
		var marketID []byte
		err := tx.QueryRow(ctx, `
			SELECT side, outcome, locked, market_id FROM orders
			WHERE order_hash = $1 AND maker = $2 AND status = 'live'
			FOR UPDATE`, hash[:], wallet).Scan(&side, &outcome, &locked, &marketID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET status = 'cancelled', locked = 0 WHERE order_hash = $1`,
			hash[:]); err != nil {
			return err
		}
		return releaseLock(ctx, tx, wallet, marketID, uint8(side), uint8(outcome), locked)
	})
}

func releaseLock(ctx context.Context, tx pgx.Tx, wallet string, marketID []byte, side, outcome uint8, locked int64) error {
	if locked == 0 {
		return nil
	}
	if side == models.SideBuy {
		_, err := tx.Exec(ctx, `
			UPDATE balances
			SET usdc_available = usdc_available + $2, usdc_locked = usdc_locked - $2
			WHERE wallet = $1`, wallet, locked)
		return err
	}
	col := "yes"
	if outcome == models.OutcomeNo {
		col = "no"
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE positions_cache SET %[1]s_locked = %[1]s_locked - $3
		WHERE "user" = $1 AND market_id = $2`, col),
		wallet, marketID, locked)
	return err
}

// legDelta captures how one side of a fill changes the mirror ledger.
// Money semantics mirror programs/pitchmarket/src/lib.rs exactly:
//   - NORMAL executes at f.Price (the maker's limit) for both sides;
//   - MINT/MERGE execute each order at its OWN limit price (pool absorbs surplus).
//
// Fees: contract M1 charges the output asset; the mirror charges USDC computed at
// the order's limit price instead (identical at the demo's fee_bps=0, and keeps
// per-fill unlock arithmetic exact — models.Fee is linear in size, so partial
// fills sum to the entry lock with zero dust).
type legDelta struct {
	wallet    string
	isBuy     bool
	outcome   uint8
	unlock    uint64 // released from the entry soft-lock
	usdcMove  uint64 // BUY: spent from lock; SELL: credited to available
	tokenMove uint64 // shares gained (BUY) or burned/transferred away (SELL)
	execPrice uint16 // per-share execution price, for avg_cost
}

func legDeltaFor(o *models.Order, f matching.Fill) legDelta {
	execPrice := f.Price // NORMAL: maker's limit, both sides
	if f.MatchType != models.MatchNormal {
		execPrice = o.Price // MINT/MERGE: own limit each (lib.rs settle_mint/merge)
	}
	d := legDelta{
		wallet:    models.PubkeyString(o.Maker),
		isBuy:     o.Side == models.SideBuy,
		outcome:   o.Outcome,
		tokenMove: f.Size,
		execPrice: execPrice,
	}
	fee := models.Fee(o.FeeBps, o.Price, f.Size)
	if d.isBuy {
		d.unlock = uint64(o.Price)*f.Size*models.MicroPerCent + fee
		d.usdcMove = uint64(execPrice)*f.Size*models.MicroPerCent + fee
	} else {
		d.unlock = f.Size // token units
		d.usdcMove = uint64(execPrice)*f.Size*models.MicroPerCent - min(fee, uint64(execPrice)*f.Size*models.MicroPerCent)
	}
	return d
}

// ApplyFill records one engine fill and mirrors its money movement: decrements
// both orders' remaining/locked, moves buyer USDC lock → seller (or pool),
// updates positions. Returns the fill row id for later settle-tx attribution.
func (s *Store) ApplyFill(ctx context.Context, f matching.Fill) (string, error) {
	var fillID string
	takerHash, makerHash := f.Taker.Hash, f.Maker.Hash

	err := s.tx(ctx, func(tx pgx.Tx) error {
		for _, side := range []struct {
			hash [32]byte
			o    *models.Order
		}{{takerHash, f.Taker.Order}, {makerHash, f.Maker.Order}} {
			d := legDeltaFor(side.o, f)

			res, err := tx.Exec(ctx, `
				UPDATE orders
				SET remaining = remaining - $2,
				    locked = locked - $3,
				    status = CASE WHEN remaining - $2 = 0 THEN 'matched' ELSE status END
				WHERE order_hash = $1 AND remaining >= $2 AND locked >= $3`,
				side.hash[:], int64(f.Size), int64(d.unlock))
			if err != nil {
				return fmt.Errorf("store: decrement order: %w", err)
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("store: order %x over-fill or missing", side.hash[:4])
			}

			if d.isBuy {
				// Spend from lock; refund the (limit − exec) improvement to available.
				refund := d.unlock - d.usdcMove
				if _, err := tx.Exec(ctx, `
					UPDATE balances
					SET usdc_locked = usdc_locked - $2, usdc_available = usdc_available + $3
					WHERE wallet = $1`, d.wallet, int64(d.unlock), int64(refund)); err != nil {
					return fmt.Errorf("store: spend buyer lock: %w", err)
				}
				if err := applyPositionDelta(ctx, tx, d.wallet, f.MarketID[:], d.outcome,
					int64(d.tokenMove), 0, d.execPrice); err != nil {
					return err
				}
			} else {
				// Tokens leave the wallet (transferred NORMAL / burned MERGE); USDC arrives.
				if err := applyPositionDelta(ctx, tx, d.wallet, f.MarketID[:], d.outcome,
					-int64(d.tokenMove), -int64(d.unlock), 0); err != nil {
					return err
				}
				// Realized PnL: proceeds vs the running average cost basis.
				if _, err := tx.Exec(ctx, fmt.Sprintf(`
					UPDATE positions_cache
					SET realized = realized + ($3::bigint - avg_cost) * $4::bigint * %d
					WHERE "user" = $1 AND market_id = $2`, models.MicroPerCent),
					d.wallet, f.MarketID[:], int64(d.execPrice), int64(d.tokenMove)); err != nil {
					return fmt.Errorf("store: realized pnl: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO balances (wallet, usdc_available) VALUES ($1, $2)
					ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2`,
					d.wallet, int64(d.usdcMove)); err != nil {
					return fmt.Errorf("store: credit seller: %w", err)
				}
			}
		}

		return tx.QueryRow(ctx, `
			INSERT INTO fills (market_id, taker_hash, maker_hash, price, size, match_type)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			f.MarketID[:], takerHash[:], makerHash[:], int16(f.Price), int64(f.Size),
			matchTypeString(f.MatchType)).Scan(&fillID)
	})
	return fillID, err
}

// RevertFill undoes ApplyFill's mirror bookkeeping after the corresponding
// settle_match tx REVERTED on-chain (interface-contract.md §6.2 revert→reconcile):
// the fill never happened, so restore remaining/locks/positions and delete the row.
// The caller separately restores the in-memory book (matching.Book.Restore).
func (s *Store) RevertFill(ctx context.Context, fillID string, f matching.Fill) error {
	takerHash, makerHash := f.Taker.Hash, f.Maker.Hash
	return s.tx(ctx, func(tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM fills WHERE id = $1`, fillID)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrNotFound
		}

		for _, side := range []struct {
			hash [32]byte
			o    *models.Order
		}{{takerHash, f.Taker.Order}, {makerHash, f.Maker.Order}} {
			d := legDeltaFor(side.o, f)

			if _, err := tx.Exec(ctx, `
				UPDATE orders
				SET remaining = remaining + $2, locked = locked + $3,
				    status = CASE WHEN status = 'matched' THEN 'live' ELSE status END
				WHERE order_hash = $1`,
				side.hash[:], int64(f.Size), int64(d.unlock)); err != nil {
				return err
			}

			if d.isBuy {
				refund := d.unlock - d.usdcMove
				if _, err := tx.Exec(ctx, `
					UPDATE balances
					SET usdc_locked = usdc_locked + $2, usdc_available = usdc_available - $3
					WHERE wallet = $1`, d.wallet, int64(d.unlock), int64(refund)); err != nil {
					return err
				}
				if err := applyPositionDelta(ctx, tx, d.wallet, f.MarketID[:], d.outcome,
					-int64(d.tokenMove), 0, 0); err != nil {
					return err
				}
			} else {
				if err := applyPositionDelta(ctx, tx, d.wallet, f.MarketID[:], d.outcome,
					int64(d.tokenMove), int64(d.unlock), 0); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, fmt.Sprintf(`
					UPDATE positions_cache
					SET realized = realized - ($3::bigint - avg_cost) * $4::bigint * %d
					WHERE "user" = $1 AND market_id = $2`, models.MicroPerCent),
					d.wallet, f.MarketID[:], int64(d.execPrice), int64(d.tokenMove)); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `
					UPDATE balances SET usdc_available = usdc_available - $2 WHERE wallet = $1`,
					d.wallet, int64(d.usdcMove)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// applyPositionDelta upserts positions_cache. tokenDelta moves the outcome
// balance; lockDelta moves the same outcome's _locked column; execPrice != 0
// updates the running avg_cost for acquisitions.
func applyPositionDelta(ctx context.Context, tx pgx.Tx, wallet string, marketID []byte,
	outcome uint8, tokenDelta, lockDelta int64, execPrice uint16) error {
	col := "yes"
	if outcome == models.OutcomeNo {
		col = "no"
	}
	avgExpr := "positions_cache.avg_cost"
	if execPrice != 0 && tokenDelta > 0 {
		// running average in price-cents over the acquired outcome (casts are
		// required: simple query protocol sends params as untyped text)
		avgExpr = fmt.Sprintf(`CASE WHEN positions_cache.%[1]s + $3::bigint > 0
			THEN (positions_cache.avg_cost * positions_cache.%[1]s + $5::bigint * $3::bigint)
			     / (positions_cache.%[1]s + $3::bigint)
			ELSE 0 END`, col)
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO positions_cache ("user", market_id, %[1]s, %[1]s_locked, avg_cost)
		VALUES ($1, $2, GREATEST($3::bigint, 0), GREATEST($4::bigint, 0), $5::bigint)
		ON CONFLICT ("user", market_id) DO UPDATE
		SET %[1]s = positions_cache.%[1]s + $3::bigint,
		    %[1]s_locked = positions_cache.%[1]s_locked + $4::bigint,
		    avg_cost = %[2]s`, col, avgExpr),
		wallet, marketID, tokenDelta, lockDelta, int64(execPrice))
	if err != nil {
		return fmt.Errorf("store: position delta: %w", err)
	}
	return nil
}

// SyncOrderChainState overwrites the mirror's fill-accounting with the observed
// on-chain OrderStatus — the chain wins every divergence (interface-contract §6.2).
func (s *Store) SyncOrderChainState(ctx context.Context, hash [32]byte, remaining uint64, closed bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders
		SET remaining = $2,
		    status = CASE
		        WHEN $3::boolean AND $2::bigint > 0 THEN 'cancelled'
		        WHEN $3::boolean THEN 'matched'
		        ELSE status END
		WHERE order_hash = $1`, hash[:], int64(remaining), closed)
	return err
}

// SetFillSettleTx records the confirmed on-chain settle_match signature.
func (s *Store) SetFillSettleTx(ctx context.Context, fillID, txSig string) error {
	res, err := s.pool.Exec(ctx, `UPDATE fills SET settle_tx = $2 WHERE id = $1`, fillID, txSig)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const orderColumns = `order_hash, market_id, maker, outcome, side, price, size, remaining, fee_bps,
	COALESCE(EXTRACT(EPOCH FROM expiry)::bigint, 0), salt, sig, locked, status, created_seq`

// LiveOrders returns the live book rows for a market (book rebuild on restart).
func (s *Store) LiveOrders(ctx context.Context, marketID [32]byte) ([]OrderRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE market_id = $1 AND status = 'live' AND remaining > 0
		ORDER BY created_seq`, marketID[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

// OrdersByMaker returns all orders for a wallet (portfolio open-orders section).
func (s *Store) OrdersByMaker(ctx context.Context, wallet string) ([]OrderRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM orders WHERE maker = $1 ORDER BY created_seq DESC LIMIT 200`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func scanOrders(rows pgx.Rows) ([]OrderRow, error) {
	var out []OrderRow
	for rows.Next() {
		var (
			r        OrderRow
			hash     []byte
			marketID []byte
			maker    string
			sig      []byte

			outcome, side, price, feeBps               int16
			size, remaining, expiry, salt, locked, seq int64
		)
		if err := rows.Scan(&hash, &marketID, &maker, &outcome, &side, &price, &size, &remaining,
			&feeBps, &expiry, &salt, &sig, &locked, &r.Status, &seq); err != nil {
			return nil, err
		}
		copy(r.OrderHash[:], hash)
		pk, err := models.ParsePubkey(maker)
		if err != nil {
			return nil, err
		}
		r.Order = models.Order{
			Maker:   pk,
			Outcome: uint8(outcome),
			Side:    uint8(side),
			Price:   uint16(price),
			Size:    uint64(size),
			FeeBps:  uint16(feeBps),
			Expiry:  expiry,
			Salt:    uint64(salt),
		}
		copy(r.Order.MarketID[:], marketID)
		copy(r.Order.Sig[:], sig)
		r.Remaining = uint64(remaining)
		r.Locked = uint64(locked)
		r.Seq = seq
		out = append(out, r)
	}
	return out, rows.Err()
}

func matchTypeString(m models.MatchType) string {
	switch m {
	case models.MatchMint:
		return "MINT"
	case models.MatchMerge:
		return "MERGE"
	default:
		return "NORMAL"
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
