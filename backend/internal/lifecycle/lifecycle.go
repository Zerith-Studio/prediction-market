// Package lifecycle drives a match end-to-end: register the fixture → auto-create
// its template markets → stream feed events (match_state over WS, fair-price
// hints to the MM bot) → at full time, resolve every market, settle precision
// pools, and sweep combos. TxODDS (or the replay recording) is the sole driver —
// "TxLINE drives auto market creation, live pricing, and resolution"
// (PROJECT_PLAN §1).
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// ChainResolver submits resolve_market on-chain (tier-a exists in the program).
// Noop mode returns "" while nothing is deployed.
type ChainResolver interface {
	ResolveMarket(ctx context.Context, marketID [32]byte, outcome uint8) (txSig string, err error)
}

type NoopChainResolver struct{}

func (NoopChainResolver) ResolveMarket(context.Context, [32]byte, uint8) (string, error) {
	return "", nil
}

// FairPriceSink receives TxLINE-implied fair prices (the MM bot implements this).
type FairPriceSink interface {
	OnFairPrice(marketID [32]byte, priceCents uint16)
}

// ChainCreator initializes markets on-chain at fixture registration
// (crank.ChainOps implements it; nil = off-chain mirror mode).
type ChainCreator interface {
	InitializeMarket(ctx context.Context, marketID [32]byte) (txSig string, err error)
}

type Service struct {
	store    *store.Store
	hub      *ws.Hub
	rfq      *rfq.Service
	resolver ChainResolver
	prices   FairPriceSink // may be nil
	log      *slog.Logger

	// PrecisionRakeBps is withheld from precision pools (ADR 0006 C1 guard #2).
	PrecisionRakeBps uint32
	// Creator, when set, mirrors every registered market on-chain.
	Creator ChainCreator
}

func New(st *store.Store, hub *ws.Hub, rfqSvc *rfq.Service, resolver ChainResolver, prices FairPriceSink, log *slog.Logger) *Service {
	if resolver == nil {
		resolver = NoopChainResolver{}
	}
	return &Service{
		store: st, hub: hub, rfq: rfqSvc, resolver: resolver, prices: prices, log: log,
		PrecisionRakeBps: 200,
	}
}

// RegisterFixture creates the match row plus one market per registry template
// (auto market creation). Idempotent.
func (s *Service) RegisterFixture(ctx context.Context, fixtureID, home, away string, kickoff time.Time) error {
	matchID, err := s.store.UpsertMatch(ctx, fixtureID, home, away, kickoff)
	if err != nil {
		return err
	}
	for _, t := range templates.Registry {
		marketID := templates.MarketID(fixtureID, t.Key)
		if err := s.store.CreateMarket(ctx, marketID, matchID, t.Key, t.Type,
			t.RenderTitle(home, away), t.RenderRule(home, away)); err != nil {
			return fmt.Errorf("lifecycle: create %s: %w", t.Key, err)
		}
		if s.Creator != nil && t.Type == "binary" {
			if _, err := s.Creator.InitializeMarket(ctx, marketID); err != nil {
				s.log.Error("lifecycle: on-chain initialize_market failed — market stays mirror-only",
					"template", t.Key, "err", err)
			}
		}
	}
	s.log.Info("lifecycle: fixture registered", "fixture", fixtureID, "markets", len(templates.Registry))
	return nil
}

// oddsPayload is the normalized odds event body (feed.EventOdds): implied
// probabilities in cents per template.
type oddsPayload struct {
	Prices map[string]uint16 `json:"prices"` // template_key → fair price ¢ (P of YES)
}

// FinalScore is the normalized score/full-time body.
type FinalScore struct {
	HomeGoals   int  `json:"home_goals"`
	AwayGoals   int  `json:"away_goals"`
	HTHomeGoals int  `json:"ht_home_goals,omitempty"`
	HTAwayGoals int  `json:"ht_away_goals,omitempty"`
	TotalPasses *int `json:"total_passes,omitempty"`
	Minute      int  `json:"minute,omitempty"`
	Abandoned   bool `json:"abandoned,omitempty"`
}

// RunFeed consumes one fixture's event stream until full time or ctx cancel.
func (s *Service) RunFeed(ctx context.Context, provider feed.FeedProvider, fixtureID string) error {
	events, err := provider.Stream(ctx, fixtureID)
	if err != nil {
		return err
	}
	for ev := range events {
		if err := s.handleEvent(ctx, fixtureID, ev); err != nil {
			s.log.Error("lifecycle: feed event", "fixture", fixtureID, "type", ev.Type, "err", err)
		}
	}
	return nil
}

func (s *Service) handleEvent(ctx context.Context, fixtureID string, ev feed.MatchEvent) error {
	raw, _ := json.Marshal(ev.Payload)
	switch ev.Type {
	case feed.EventKickoff:
		if err := s.store.SetMatchState(ctx, fixtureID, "live", raw); err != nil {
			return err
		}
		// Kickoff-lock: precision entry closes NOW (ADR 0006 C2, non-negotiable).
		s.closePrecision(ctx, fixtureID)

	case feed.EventScore:
		if err := s.store.SetMatchState(ctx, fixtureID, "live", raw); err != nil {
			return err
		}

	case feed.EventOdds:
		var odds oddsPayload
		if err := json.Unmarshal(raw, &odds); err != nil {
			return err
		}
		if s.prices != nil {
			for key, price := range odds.Prices {
				if price >= 1 && price <= 99 {
					s.prices.OnFairPrice(templates.MarketID(fixtureID, key), price)
				}
			}
		}

	case feed.EventFullTime:
		if err := s.store.SetMatchState(ctx, fixtureID, "finished", raw); err != nil {
			return err
		}
		var final FinalScore
		if err := json.Unmarshal(raw, &final); err != nil {
			return err
		}
		if err := s.ResolveFixture(ctx, fixtureID, final); err != nil {
			return err
		}
	}

	s.hub.Broadcast(ws.Event{
		Type:      ws.EventMatchState,
		FixtureID: fixtureID,
		Data:      map[string]any{"event": ev.Type, "payload": ev.Payload},
	})
	return nil
}

func (s *Service) closePrecision(ctx context.Context, fixtureID string) {
	for _, t := range templates.Registry {
		if t.Type != "precision" {
			continue
		}
		marketID := templates.MarketID(fixtureID, t.Key)
		if err := s.store.SetMarketStatus(ctx, marketID, "closed"); err != nil && err != store.ErrNotFound {
			s.log.Error("lifecycle: close precision", "template", t.Key, "err", err)
		}
	}
}

// ResolveFixture settles every market of a finished match from the final
// score, then sweeps combos. Binary outcomes also go through the on-chain
// resolver (tier-a; Noop until deployed).
func (s *Service) ResolveFixture(ctx context.Context, fixtureID string, final FinalScore) error {
	if final.Abandoned {
		return s.voidFixture(ctx, fixtureID)
	}
	for _, t := range templates.Registry {
		marketID := templates.MarketID(fixtureID, t.Key)
		switch t.Type {
		case "binary":
			result, ok := binaryOutcome(t.Key, final)
			if !ok {
				continue
			}
			outcome := map[string]uint8{"no": 0, "yes": 1, "void": 2}[result]
			status := "settled"
			if result == "void" {
				status = "void"
			}
			txSig, err := s.resolver.ResolveMarket(ctx, marketID, outcome)
			if err != nil {
				s.log.Error("lifecycle: on-chain resolve failed", "template", t.Key, "err", err)
				// keep going: off-chain state still resolves; index reconciles later
			}
			outcomeJSON, _ := json.Marshal(map[string]any{
				"result": result,
				"score":  fmt.Sprintf("%d-%d", final.HomeGoals, final.AwayGoals),
			})
			if err := s.store.SettleMarket(ctx, marketID, outcomeJSON, txSig, status); err != nil {
				return err
			}

		case "precision":
			actual, ok := precisionActual(t.Key, final)
			if !ok {
				continue
			}
			outcomeJSON, _ := json.Marshal(map[string]any{"actual": actual})
			if _, err := s.store.SettlePrecision(ctx, marketID, actual, t.Scale, 2,
				s.PrecisionRakeBps, outcomeJSON); err != nil && err != store.ErrNotFound {
				return err
			}
		}
	}
	if s.rfq != nil {
		if err := s.rfq.ResolveSettled(ctx); err != nil {
			return err
		}
	}
	s.log.Info("lifecycle: fixture resolved", "fixture", fixtureID,
		"score", fmt.Sprintf("%d-%d", final.HomeGoals, final.AwayGoals))
	return nil
}

func (s *Service) voidFixture(ctx context.Context, fixtureID string) error {
	for _, t := range templates.Registry {
		marketID := templates.MarketID(fixtureID, t.Key)
		var err error
		if t.Type == "precision" {
			err = s.store.VoidPrecision(ctx, marketID)
		} else {
			outcomeJSON, _ := json.Marshal(map[string]any{"result": "void"})
			err = s.store.SettleMarket(ctx, marketID, outcomeJSON, "", "void")
		}
		if err != nil && err != store.ErrNotFound {
			return err
		}
	}
	if s.rfq != nil {
		return s.rfq.ResolveSettled(ctx)
	}
	return nil
}

// binaryOutcome resolves a template to "yes" | "no" | "void" from the final
// score. dnb_home VOIDs on the draw (stake refunded — the on-chain program's
// MarketOutcome::Void path).
func binaryOutcome(templateKey string, f FinalScore) (string, bool) {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	switch templateKey {
	case "home_win":
		return yn(f.HomeGoals > f.AwayGoals), true
	case "draw":
		return yn(f.HomeGoals == f.AwayGoals), true
	case "away_win":
		return yn(f.AwayGoals > f.HomeGoals), true
	case "over_2_5":
		return yn(f.HomeGoals+f.AwayGoals >= 3), true
	case "btts":
		return yn(f.HomeGoals > 0 && f.AwayGoals > 0), true
	case "dnb_home":
		if f.HomeGoals == f.AwayGoals {
			return "void", true
		}
		return yn(f.HomeGoals > f.AwayGoals), true
	case "ou_1h_075":
		return yn(f.HTHomeGoals+f.HTAwayGoals >= 1), true
	}
	return "", false
}

func precisionActual(templateKey string, f FinalScore) (float64, bool) {
	switch templateKey {
	case "precision_total_goals":
		return float64(f.HomeGoals + f.AwayGoals), true
	case "precision_total_passes":
		if f.TotalPasses == nil {
			return 0, false
		}
		return float64(*f.TotalPasses), true
	}
	return 0, false
}
