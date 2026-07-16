package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

type RFQRow struct {
	ID        string       `json:"id"`
	Taker     string       `json:"taker"`
	Legs      []models.Leg `json:"legs"`
	Stake     uint64       `json:"stake"`
	Status    string       `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
}

type QuoteRow struct {
	QuoteHash [32]byte     `json:"-"`
	RFQID     string       `json:"rfq_id,omitempty"`
	Maker     string       `json:"maker"`
	Legs      []models.Leg `json:"legs"`
	Stake     uint64       `json:"stake"`
	Payout    uint64       `json:"payout"`
	Expiry    time.Time    `json:"expiry"`
	Salt      uint64       `json:"salt"`
	Sig       [64]byte     `json:"-"`
	Status    string       `json:"status"`
}

type EscrowRow struct {
	QuoteHash  [32]byte     `json:"-"`
	Taker      string       `json:"taker"`
	Status     string       `json:"status"`
	Stake      uint64       `json:"stake"`
	Payout     uint64       `json:"payout"`
	Legs       int          `json:"legs"`        // count
	LegDetails []models.Leg `json:"-"`            // the actual legs (portfolio detail)
	AcceptTx   string       `json:"accept_tx,omitempty"`
	ResolveTx  string       `json:"resolve_tx,omitempty"`
}

// legsJSON stores legs with hex market ids (BYTEA doesn't fit inside JSONB).
type legJSON struct {
	MarketID string `json:"market_id"`
	Outcome  uint8  `json:"outcome"`
}

// encodeLegs returns a JSON string (not []byte — pgx's simple protocol would
// send []byte as bytea hex, which JSONB rejects).
func encodeLegs(legs []models.Leg) (string, error) {
	out := make([]legJSON, len(legs))
	for i, l := range legs {
		out[i] = legJSON{MarketID: models.HashString(l.MarketID), Outcome: l.Outcome}
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

func decodeLegs(raw []byte) ([]models.Leg, error) {
	var in []legJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := make([]models.Leg, len(in))
	for i, l := range in {
		id, err := models.ParseHash(l.MarketID)
		if err != nil {
			return nil, fmt.Errorf("store: leg %d: %w", i, err)
		}
		out[i] = models.Leg{MarketID: id, Outcome: l.Outcome}
	}
	return out, nil
}

// CreateRFQ opens a request-for-quote on a leg combination (POST /combos).
func (s *Store) CreateRFQ(ctx context.Context, taker string, legs []models.Leg, stake uint64) (string, error) {
	raw, err := encodeLegs(legs)
	if err != nil {
		return "", err
	}
	var id string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO combo_rfqs (taker, legs, stake) VALUES ($1,$2,$3) RETURNING id`,
		taker, raw, int64(stake)).Scan(&id)
	return id, err
}

func (s *Store) GetRFQ(ctx context.Context, id string) (RFQRow, error) {
	var r RFQRow
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, taker, legs, stake, status, created_at FROM combo_rfqs WHERE id = $1`,
		id).Scan(&r.ID, &r.Taker, &raw, &r.Stake, &r.Status, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Legs, err = decodeLegs(raw)
	return r, err
}

// OpenRFQs lists RFQs awaiting quotes (the MM bot polls this).
func (s *Store) OpenRFQs(ctx context.Context) ([]RFQRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, taker, legs, stake, status, created_at FROM combo_rfqs
		WHERE status = 'open' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RFQRow
	for rows.Next() {
		var r RFQRow
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Taker, &raw, &r.Stake, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		if r.Legs, err = decodeLegs(raw); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertQuote records a signed MM quote against an RFQ and flips the RFQ to 'quoted'.
func (s *Store) InsertQuote(ctx context.Context, q *models.ComboQuote, rfqID string) error {
	hash := models.QuoteHash(q)
	raw, err := encodeLegs(q.Legs)
	if err != nil {
		return err
	}
	var expiry *time.Time
	if q.Expiry != 0 {
		t := time.Unix(q.Expiry, 0).UTC()
		expiry = &t
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO combo_quotes (quote_hash, rfq_id, maker, legs, stake, payout, expiry, salt, sig)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			hash[:], rfqID, models.PubkeyString(q.Maker), raw,
			int64(q.Stake), int64(q.Payout), expiry, int64(q.Salt), q.Sig[:])
		if isUniqueViolation(err) {
			return ErrDuplicateOrder
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE combo_rfqs SET status = 'quoted' WHERE id = $1 AND status = 'open'`, rfqID)
		return err
	})
}

func (s *Store) GetQuote(ctx context.Context, hash [32]byte) (QuoteRow, error) {
	var q QuoteRow
	var raw, sig []byte
	var quoteHash []byte
	var rfqID *string
	var expiry *time.Time
	var salt int64
	err := s.pool.QueryRow(ctx, `
		SELECT quote_hash, COALESCE(rfq_id::text, ''), maker, legs, stake, payout, expiry, salt, sig, status
		FROM combo_quotes WHERE quote_hash = $1`, hash[:]).
		Scan(&quoteHash, &rfqID, &q.Maker, &raw, &q.Stake, &q.Payout, &expiry, &salt, &sig, &q.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, ErrNotFound
	}
	if err != nil {
		return q, err
	}
	copy(q.QuoteHash[:], quoteHash)
	copy(q.Sig[:], sig)
	q.Salt = uint64(salt)
	if rfqID != nil {
		q.RFQID = *rfqID
	}
	if expiry != nil {
		q.Expiry = *expiry
	}
	q.Legs, err = decodeLegs(raw)
	return q, err
}

// QuotesForRFQ lists quotes answering an RFQ (GET /combos/:id).
func (s *Store) QuotesForRFQ(ctx context.Context, rfqID string) ([]QuoteRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT quote_hash, maker, legs, stake, payout, expiry, salt, sig, status
		FROM combo_quotes WHERE rfq_id = $1`, rfqID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuoteRow
	for rows.Next() {
		var q QuoteRow
		var raw, sig, quoteHash []byte
		var expiry *time.Time
		var salt int64
		if err := rows.Scan(&quoteHash, &q.Maker, &raw, &q.Stake, &q.Payout, &expiry, &salt, &sig, &q.Status); err != nil {
			return nil, err
		}
		copy(q.QuoteHash[:], quoteHash)
		copy(q.Sig[:], sig)
		q.Salt = uint64(salt)
		if expiry != nil {
			q.Expiry = *expiry
		}
		if q.Legs, err = decodeLegs(raw); err != nil {
			return nil, err
		}
		q.RFQID = rfqID
		out = append(out, q)
	}
	return out, rows.Err()
}

// AcceptQuote mirrors combo_accept (ADR 0004): atomically flip the quote
// open→accepted (single-use salt semantics — a second accept fails), debit the
// taker's stake and the MM's risk (payout−stake) into escrow, open the escrow row.
// On-chain, the atomic vault debit is the security boundary; this is the UX mirror.
func (s *Store) AcceptQuote(ctx context.Context, quoteHash [32]byte, taker string, acceptTx string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var stake, payout int64
		var maker string
		var rfqID *string
		err := tx.QueryRow(ctx, `
			UPDATE combo_quotes SET status = 'accepted'
			WHERE quote_hash = $1 AND status = 'open'
			  AND (expiry IS NULL OR expiry > now())
			RETURNING maker, stake, payout, rfq_id::text`, quoteHash[:]).
			Scan(&maker, &stake, &payout, &rfqID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrQuoteNotOpen
		}
		if err != nil {
			return err
		}

		// Taker pays stake; MM pays risk = payout − stake. Both leave available
		// (the full pot P sits in the on-chain ComboEscrow PDA, not in a vault).
		res, err := tx.Exec(ctx, `
			UPDATE balances SET usdc_available = usdc_available - $2
			WHERE wallet = $1 AND usdc_available >= $2`, taker, stake)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrInsufficientFunds
		}
		risk := payout - stake
		res, err = tx.Exec(ctx, `
			UPDATE balances SET usdc_available = usdc_available - $2
			WHERE wallet = $1 AND usdc_available >= $2`, maker, risk)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrInsufficientFunds
		}

		if rfqID != nil {
			if _, err := tx.Exec(ctx,
				`UPDATE combo_rfqs SET status = 'accepted' WHERE id = $1`, *rfqID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO combo_escrows (quote_hash, taker, status, accept_tx)
			VALUES ($1,$2,'accepted',$3)`, quoteHash[:], taker, nullable(acceptTx))
		return err
	})
}

// ResolveEscrow settles a combo (ADR 0004 resolve semantics): won → taker gets
// the full pot; lost → MM gets it back; void → both refunded pro-rata.
func (s *Store) ResolveEscrow(ctx context.Context, quoteHash [32]byte, outcome string, resolveTx string) error {
	if outcome != "won" && outcome != "lost" && outcome != "void" {
		return fmt.Errorf("store: bad escrow outcome %q", outcome)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		var taker, maker string
		var stake, payout int64
		err := tx.QueryRow(ctx, `
			SELECT e.taker, q.maker, q.stake, q.payout
			FROM combo_escrows e JOIN combo_quotes q ON q.quote_hash = e.quote_hash
			WHERE e.quote_hash = $1 AND e.status = 'accepted'
			FOR UPDATE OF e`, quoteHash[:]).Scan(&taker, &maker, &stake, &payout)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		credit := func(wallet string, amount int64) error {
			if amount == 0 {
				return nil
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO balances (wallet, usdc_available) VALUES ($1,$2)
				ON CONFLICT (wallet) DO UPDATE SET usdc_available = balances.usdc_available + $2`,
				wallet, amount)
			return err
		}
		switch outcome {
		case "won":
			err = credit(taker, payout)
		case "lost":
			err = credit(maker, payout)
		case "void":
			if err = credit(taker, stake); err == nil {
				err = credit(maker, payout-stake)
			}
		}
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE combo_escrows SET status = $2, resolve_tx = $3 WHERE quote_hash = $1`,
			quoteHash[:], outcome, nullable(resolveTx))
		return err
	})
}

// EscrowsForWallet lists combos where the wallet is taker or MM (portfolio).
func (s *Store) EscrowsForWallet(ctx context.Context, wallet string) ([]EscrowRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.quote_hash, e.taker, e.status, COALESCE(e.accept_tx,''), COALESCE(e.resolve_tx,''),
		       q.stake, q.payout, q.legs
		FROM combo_escrows e JOIN combo_quotes q ON q.quote_hash = e.quote_hash
		WHERE e.taker = $1 OR q.maker = $1
		ORDER BY e.quote_hash`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EscrowRow
	for rows.Next() {
		var e EscrowRow
		var qh, legsRaw []byte
		if err := rows.Scan(&qh, &e.Taker, &e.Status, &e.AcceptTx, &e.ResolveTx,
			&e.Stake, &e.Payout, &legsRaw); err != nil {
			return nil, err
		}
		copy(e.QuoteHash[:], qh)
		if e.LegDetails, err = decodeLegs(legsRaw); err != nil {
			return nil, err
		}
		e.Legs = len(e.LegDetails)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AcceptedEscrow pairs an open escrow with its quote for the resolve sweep.
type AcceptedEscrow struct {
	Quote QuoteRow
	Taker string
}

// AcceptedEscrows lists escrows still awaiting resolution.
func (s *Store) AcceptedEscrows(ctx context.Context) ([]AcceptedEscrow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT q.quote_hash, q.maker, q.legs, q.stake, q.payout, q.salt, q.sig, e.taker
		FROM combo_escrows e JOIN combo_quotes q ON q.quote_hash = e.quote_hash
		WHERE e.status = 'accepted'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AcceptedEscrow
	for rows.Next() {
		var a AcceptedEscrow
		var qh, raw, sig []byte
		var salt int64
		if err := rows.Scan(&qh, &a.Quote.Maker, &raw, &a.Quote.Stake, &a.Quote.Payout,
			&salt, &sig, &a.Taker); err != nil {
			return nil, err
		}
		copy(a.Quote.QuoteHash[:], qh)
		copy(a.Quote.Sig[:], sig)
		a.Quote.Salt = uint64(salt)
		if a.Quote.Legs, err = decodeLegs(raw); err != nil {
			return nil, err
		}
		a.Quote.Status = "accepted"
		out = append(out, a)
	}
	return out, rows.Err()
}

// ExpireQuotes flips expired open quotes and returns how many were expired.
func (s *Store) ExpireQuotes(ctx context.Context) (int64, error) {
	res, err := s.pool.Exec(ctx, `
		UPDATE combo_quotes SET status = 'expired'
		WHERE status = 'open' AND expiry IS NOT NULL AND expiry <= now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
