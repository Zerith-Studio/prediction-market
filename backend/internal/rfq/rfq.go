// Package rfq runs the combo lifecycle (ADR 0004): a taker requests quotes on
// N legs, MMs answer with signed ComboQuotes, the taker accepts one, and the
// escrow resolves from the same per-leg outcomes binary redemption reads.
// Negotiation is off-chain; escrow/resolution belong on-chain (combo_accept —
// currently stubbed in the program, so ComboSubmitter has a Noop mode).
package rfq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

var (
	ErrBadLegs      = errors.New("rfq: combo needs 2..6 distinct binary legs")
	ErrMutexConflict = errors.New("rfq: legs are mutually exclusive (same mutex group, same match)")
	ErrLegNotOpen   = errors.New("rfq: leg market is not open")
	ErrBadSignature = errors.New("rfq: quote signature does not verify")
	ErrExpired      = errors.New("rfq: quote expiry must be in the future")
	ErrLegsMismatch = errors.New("rfq: quote legs differ from the RFQ's legs")
)

const MaxLegs = 6 // ComboEscrow::MAX_LEGS in programs/pitchmarket/src/state.rs

// ComboSubmitter submits the on-chain combo_accept (interface-contract §4).
type ComboSubmitter interface {
	ComboAccept(ctx context.Context, q models.ComboQuote, taker [32]byte) (txSig string, err error)
}

// NoopComboSubmitter keeps combos off-chain — the PROJECT_PLAN §7 cut position
// while the program's combo_accept is a stub. Swap for a crank-backed
// implementation when E1 lands it.
type NoopComboSubmitter struct{}

func (NoopComboSubmitter) ComboAccept(context.Context, models.ComboQuote, [32]byte) (string, error) {
	return "", nil
}

type Service struct {
	store *store.Store
	hub   *ws.Hub
	sub   ComboSubmitter
	log   *slog.Logger
}

func New(st *store.Store, hub *ws.Hub, sub ComboSubmitter, log *slog.Logger) *Service {
	if sub == nil {
		sub = NoopComboSubmitter{}
	}
	return &Service{store: st, hub: hub, sub: sub, log: log}
}

// ValidateLegs enforces the combo builder rules: 2..6 distinct open binary
// markets, and no two legs from the same mutex group on the same match
// (ADR 0004 "compat mutex groups" — the UI greys these out; the API refuses).
func (s *Service) ValidateLegs(ctx context.Context, legs []models.Leg) error {
	if len(legs) < 2 || len(legs) > MaxLegs {
		return ErrBadLegs
	}
	seen := make(map[[32]byte]bool, len(legs))
	type groupKey struct{ matchID, group string }
	groups := make(map[groupKey]bool)
	for _, leg := range legs {
		if seen[leg.MarketID] {
			return ErrBadLegs
		}
		seen[leg.MarketID] = true

		m, err := s.store.GetMarket(ctx, leg.MarketID)
		if err != nil {
			return fmt.Errorf("%w: %x", ErrLegNotOpen, leg.MarketID[:4])
		}
		if m.Status != "open" || m.Type != "binary" {
			return fmt.Errorf("%w: %s (%s)", ErrLegNotOpen, m.Title, m.Status)
		}
		if t, ok := templates.ByKey(m.TemplateKey); ok && t.MutexGroup != "" {
			k := groupKey{m.MatchID, t.MutexGroup}
			if groups[k] {
				return ErrMutexConflict
			}
			groups[k] = true
		}
	}
	return nil
}

// CreateRFQ validates and opens a request-for-quote (POST /combos).
func (s *Service) CreateRFQ(ctx context.Context, taker string, legs []models.Leg, stake uint64) (string, error) {
	if err := s.ValidateLegs(ctx, legs); err != nil {
		return "", err
	}
	if stake == 0 {
		return "", ErrBadLegs
	}
	return s.store.CreateRFQ(ctx, taker, legs, stake)
}

// SubmitQuote verifies and records an MM's signed quote, then broadcasts it
// (the combo builder's countdown card).
func (s *Service) SubmitQuote(ctx context.Context, q *models.ComboQuote, rfqID string) error {
	if !models.VerifyQuoteSig(q) {
		return ErrBadSignature
	}
	if q.Expiry != 0 && q.Expiry <= time.Now().Unix() {
		return ErrExpired
	}
	if q.Payout <= q.Stake {
		return fmt.Errorf("rfq: payout must exceed stake")
	}
	rfq, err := s.store.GetRFQ(ctx, rfqID)
	if err != nil {
		return err
	}
	if !sameLegs(rfq.Legs, q.Legs) || rfq.Stake != q.Stake {
		return ErrLegsMismatch
	}

	if err := s.store.InsertQuote(ctx, q, rfqID); err != nil {
		return err
	}
	s.hub.Broadcast(ws.Event{
		Type: ws.EventComboQuote,
		Data: map[string]any{
			"rfq_id":     rfqID,
			"quote_hash": models.HashString(models.QuoteHash(q)),
			"maker":      models.PubkeyString(q.Maker),
			"stake":      q.Stake,
			"payout":     q.Payout,
			"expiry":     q.Expiry,
		},
	})
	return nil
}

// Accept takes a quote: submit combo_accept on-chain (or Noop while stubbed),
// then mirror the escrow. Store-side AcceptQuote is the single-use guard —
// a second accept of the same quote fails with ErrQuoteNotOpen.
func (s *Service) Accept(ctx context.Context, quoteHash [32]byte, taker [32]byte) (string, error) {
	q, err := s.store.GetQuote(ctx, quoteHash)
	if err != nil {
		return "", err
	}
	quote := models.ComboQuote{
		Legs: q.Legs, Stake: q.Stake, Payout: q.Payout, Salt: q.Salt, Sig: q.Sig,
	}
	if pk, err := models.ParsePubkey(q.Maker); err == nil {
		quote.Maker = pk
	}
	if !q.Expiry.IsZero() {
		quote.Expiry = q.Expiry.Unix()
	}

	txSig, err := s.sub.ComboAccept(ctx, quote, taker)
	if err != nil {
		return "", fmt.Errorf("rfq: combo_accept: %w", err)
	}
	if err := s.store.AcceptQuote(ctx, quoteHash, models.PubkeyString(taker), txSig); err != nil {
		return "", err
	}
	s.hub.Broadcast(ws.Event{
		Type: ws.EventComboQuote,
		Data: map[string]any{
			"quote_hash": models.HashString(quoteHash),
			"status":     "accepted",
			"accept_tx":  txSig,
		},
	})
	return txSig, nil
}

// ResolveSettled sweeps accepted escrows and resolves any whose legs have all
// reached a terminal market state. Decision per ADR 0004: any leg settled
// against the taker → lost (the AND is already false); else any VOID leg →
// void (refund both); else all legs matching → won; otherwise still pending.
func (s *Service) ResolveSettled(ctx context.Context) error {
	escrows, err := s.store.AcceptedEscrows(ctx)
	if err != nil {
		return err
	}
	for _, e := range escrows {
		outcome, ready := s.comboOutcome(ctx, e.Quote.Legs)
		if !ready {
			continue
		}
		if err := s.store.ResolveEscrow(ctx, e.Quote.QuoteHash, outcome, ""); err != nil {
			s.log.Error("rfq: resolve escrow", "quote", models.HashString(e.Quote.QuoteHash), "err", err)
			continue
		}
		s.hub.Broadcast(ws.Event{
			Type: ws.EventComboQuote,
			Data: map[string]any{
				"quote_hash": models.HashString(e.Quote.QuoteHash),
				"status":     outcome,
			},
		})
	}
	return nil
}

func (s *Service) comboOutcome(ctx context.Context, legs []models.Leg) (string, bool) {
	sawVoid := false
	for _, leg := range legs {
		m, err := s.store.GetMarket(ctx, leg.MarketID)
		if err != nil {
			return "", false
		}
		switch m.Status {
		case "void":
			sawVoid = true
		case "settled":
			won, ok := legWon(m.Outcome, leg.Outcome)
			if !ok {
				return "", false
			}
			if !won {
				return "lost", true
			}
		default:
			return "", false // a leg is still live — combo pending
		}
	}
	if sawVoid {
		return "void", true
	}
	return "won", true
}

// legWon reads the binary outcome JSON ({"result":"yes"|"no"}) written by the
// resolver and compares it against the leg's chosen side.
func legWon(outcomeJSON []byte, legOutcome uint8) (won, ok bool) {
	res := parseResult(outcomeJSON)
	if res == "" {
		return false, false
	}
	want := "no"
	if legOutcome == models.OutcomeYes {
		want = "yes"
	}
	return res == want, true
}

func sameLegs(a, b []models.Leg) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		found := false
		for i, y := range b {
			if !used[i] && x == y {
				used[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
