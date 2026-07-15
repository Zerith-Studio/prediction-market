// Package mmbot is the operator's market-making bot: it keeps binary books
// two-sided around the TxLINE-implied fair price, answers combo RFQs with the
// pinned quote formula (PROJECT_PLAN §5), and crowd-seeds precision pools with
// simulated retail personas (ADR 0006 C3 — demo population, not a strategic
// player; the demo script says this out loud).
package mmbot

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

type Bot struct {
	ex    *exchange.Exchange
	store *store.Store
	rfq   *rfq.Service
	priv  ed25519.PrivateKey
	pub   [32]byte
	rng   *rand.Rand
	log   *slog.Logger

	// HalfSpread (¢) each side of fair; QuoteSize shares per side.
	HalfSpread uint16
	QuoteSize  uint64
	// Margin shaved off the fair combo payout (PROJECT_PLAN §5: quoted = fair·(1−margin)).
	Margin float64
	// CorrAdj scales the naive Πp — same-match legs are positively correlated,
	// so the true joint probability is higher and the honest payout lower.
	CorrAdj float64
	// MaxExposure caps total outstanding combo risk (payout − stake), micro-USDC.
	MaxExposure uint64
	// QuoteTTL is the accept window on RFQ quotes (the countdown on camera).
	QuoteTTL time.Duration

	mu       sync.Mutex
	fair     map[[32]byte]uint16
	live     map[[32]byte][][32]byte // bot's resting order hashes per market
	exposure uint64
}

// New seeds the bot with its own ed25519 identity. Pass a fixed rng for
// deterministic tests.
func New(ex *exchange.Exchange, st *store.Store, rfqSvc *rfq.Service,
	priv ed25519.PrivateKey, rng *rand.Rand, log *slog.Logger) *Bot {
	b := &Bot{
		ex: ex, store: st, rfq: rfqSvc, priv: priv, rng: rng, log: log,
		HalfSpread: 2, QuoteSize: 100,
		Margin: 0.10, CorrAdj: 1.15, MaxExposure: 500_000_000, // 500 USDC
		QuoteTTL: 30 * time.Second,
		fair:     make(map[[32]byte]uint16),
		live:     make(map[[32]byte][][32]byte),
	}
	copy(b.pub[:], priv.Public().(ed25519.PublicKey))
	return b
}

func (b *Bot) Wallet() string { return models.PubkeyString(b.pub) }

// Fund tops up the bot's demo balance (mirrors an operator vault deposit).
func (b *Bot) Fund(ctx context.Context, microUSDC uint64) error {
	_, err := b.store.Deposit(ctx, b.Wallet(), microUSDC)
	return err
}

// OnFairPrice implements lifecycle.FairPriceSink: every TxLINE odds tick
// re-quotes the market around the new fair.
func (b *Bot) OnFairPrice(marketID [32]byte, priceCents uint16) {
	b.mu.Lock()
	b.fair[marketID] = priceCents
	b.mu.Unlock()
	if err := b.Requote(context.Background(), marketID, priceCents); err != nil {
		b.log.Error("mmbot: requote", "err", err)
	}
}

// Requote cancels the bot's resting orders on the market and posts a fresh
// two-sided pair: BUY YES @ (fair − spread) and BUY NO @ (100 − fair − spread).
// Both sides are BUYs, so crossing takers MINT complete sets against the bot —
// the bot needs only USDC inventory, never tokens (ADR 0002 three-path model).
func (b *Bot) Requote(ctx context.Context, marketID [32]byte, fair uint16) error {
	b.mu.Lock()
	old := b.live[marketID]
	delete(b.live, marketID)
	b.mu.Unlock()
	for _, h := range old {
		if err := b.ex.Cancel(ctx, h, b.pub); err != nil && err != store.ErrNotFound {
			b.log.Warn("mmbot: cancel stale quote", "err", err)
		}
	}

	bidYes := int(fair) - int(b.HalfSpread)
	bidNo := 100 - int(fair) - int(b.HalfSpread)
	var placed [][32]byte
	for _, q := range []struct {
		outcome uint8
		price   int
	}{{models.OutcomeYes, bidYes}, {models.OutcomeNo, bidNo}} {
		if q.price < 1 || q.price > 99 {
			continue // fair too close to the boundary — stay one-sided
		}
		o := &models.Order{
			Maker:    b.pub,
			MarketID: marketID,
			Outcome:  q.outcome,
			Side:     models.SideBuy,
			Price:    uint16(q.price),
			Size:     b.QuoteSize,
			// Quotes age out: a crashed/restarted bot's stale orders leave the
			// book by themselves instead of resting forever.
			Expiry: time.Now().Add(10 * time.Minute).Unix(),
			Salt:   b.rng.Uint64(),
		}
		models.SignOrder(o, b.priv)
		hash, _, err := b.ex.SubmitOrder(ctx, o)
		if err != nil {
			return fmt.Errorf("mmbot: post quote: %w", err)
		}
		placed = append(placed, hash)
	}

	b.mu.Lock()
	b.live[marketID] = placed
	b.mu.Unlock()
	return nil
}

// QuoteRFQ answers one RFQ with a signed combo quote:
//
//	p_joint  = Π(fair_i/100) × CorrAdj   (clamped to ≤ 0.95)
//	fair_odds = 1 / p_joint
//	payout    = stake × (1 + (fair_odds − 1) × (1 − Margin))
//
// capped so payout − stake never exceeds remaining exposure budget.
func (b *Bot) QuoteRFQ(ctx context.Context, r store.RFQRow) error {
	b.mu.Lock()
	pJoint := 1.0
	for _, leg := range r.Legs {
		fair, ok := b.fair[leg.MarketID]
		if !ok {
			b.mu.Unlock()
			return fmt.Errorf("mmbot: no fair price for leg %x", leg.MarketID[:4])
		}
		p := float64(fair) / 100
		if leg.Outcome == models.OutcomeNo {
			p = 1 - p
		}
		pJoint *= p
	}
	remaining := b.MaxExposure - b.exposure
	b.mu.Unlock()

	pJoint = math.Min(pJoint*b.CorrAdj, 0.95)
	fairOdds := 1 / pJoint
	payout := uint64(float64(r.Stake) * (1 + (fairOdds-1)*(1-b.Margin)))
	if payout <= r.Stake {
		return fmt.Errorf("mmbot: quote collapsed below stake (p_joint=%.3f)", pJoint)
	}
	if risk := payout - r.Stake; risk > remaining {
		if remaining == 0 {
			return fmt.Errorf("mmbot: exposure budget exhausted")
		}
		payout = r.Stake + remaining
	}

	q := &models.ComboQuote{
		Maker:  b.pub,
		Legs:   r.Legs,
		Stake:  r.Stake,
		Payout: payout,
		Expiry: time.Now().Add(b.QuoteTTL).Unix(),
		Salt:   b.rng.Uint64(),
	}
	models.SignQuote(q, b.priv)
	if err := b.rfq.SubmitQuote(ctx, q, r.ID); err != nil {
		return err
	}
	b.mu.Lock()
	b.exposure += payout - r.Stake
	b.mu.Unlock()
	b.log.Info("mmbot: quoted RFQ", "rfq", r.ID, "stake", r.Stake, "payout", payout)
	return nil
}

// PollRFQs answers open RFQs on an interval until ctx is cancelled.
func (b *Bot) PollRFQs(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			open, err := b.store.OpenRFQs(ctx)
			if err != nil {
				b.log.Error("mmbot: list RFQs", "err", err)
				continue
			}
			for _, r := range open {
				if err := b.QuoteRFQ(ctx, r); err != nil {
					b.log.Warn("mmbot: quote RFQ", "rfq", r.ID, "err", err)
				}
			}
			if _, err := b.store.ExpireQuotes(ctx); err != nil {
				b.log.Error("mmbot: expire quotes", "err", err)
			}
		}
	}
}

// SeedPrecision populates a pool with n simulated personas, guesses drawn
// Normal(fairGuess, sigma), stakes uniform in [stakeMin, stakeMax]. Each
// persona is its own wallet — a believable bell-shaped crowd on camera, and
// exactly one entry per wallet like everyone else (ADR 0006 C3/C1).
func (b *Bot) SeedPrecision(ctx context.Context, marketID [32]byte, fairGuess, sigma float64,
	n int, stakeMin, stakeMax uint64) (int, error) {
	seeded := 0
	for i := 0; i < n; i++ {
		pub, _, err := ed25519.GenerateKey(b.rng)
		if err != nil {
			return seeded, err
		}
		var pk [32]byte
		copy(pk[:], pub)
		wallet := models.PubkeyString(pk)

		stake := stakeMin
		if stakeMax > stakeMin {
			stake += uint64(b.rng.Int63n(int64(stakeMax - stakeMin)))
		}
		if _, err := b.store.Deposit(ctx, wallet, stake); err != nil {
			return seeded, err
		}
		guess := math.Round(fairGuess + b.rng.NormFloat64()*sigma)
		if guess < 0 {
			guess = 0
		}
		if _, err := b.store.EnterPrecision(ctx, marketID, wallet, guess, stake); err != nil {
			b.log.Warn("mmbot: persona entry", "err", err)
			continue
		}
		seeded++
	}
	b.log.Info("mmbot: precision pool seeded", "market", models.HashString(marketID), "personas", seeded)
	return seeded, nil
}
