// Package oneliner generates the live ticker: every ~2 minutes per live match,
// a Claude call turns match context into 6 punchy lines per market
// (PROJECT_PLAN §3 "delight"). The Generator seam keeps tests hermetic and the
// whole feature optional — no ANTHROPIC_API_KEY, no ticker.
package oneliner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// MatchContext is what the generator sees for one market.
type MatchContext struct {
	Home, Away  string
	LiveState   string // raw live_state JSON from the feed
	MarketTitle string
	MarketRule  string
}

type Generator interface {
	Lines(ctx context.Context, mc MatchContext) ([]string, error)
}

// Claude calls the Anthropic Messages API directly (no SDK dep).
type Claude struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewClaude(apiKey string) *Claude {
	return &Claude{APIKey: apiKey, Model: "claude-haiku-4-5", Client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Claude) Lines(ctx context.Context, mc MatchContext) ([]string, error) {
	prompt := fmt.Sprintf(
		"Live football match %s vs %s. Live state: %s\nMarket: %q (%s)\n"+
			"Write exactly 6 one-liners (≤80 chars each) a sharp trader would enjoy — "+
			"witty, specific to the state of this match and this market, no hashtags, no emoji. "+
			"Return them as a JSON array of 6 strings and nothing else.",
		mc.Home, mc.Away, mc.LiveState, mc.MarketTitle, mc.MarketRule)

	body, _ := json.Marshal(map[string]any{
		"model":      c.Model,
		"max_tokens": 512,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oneliner: anthropic status %d", resp.StatusCode)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Content) == 0 {
		return nil, fmt.Errorf("oneliner: empty response")
	}
	return extractLines(out.Content[0].Text)
}

// extractLines pulls the JSON string array out of the model's reply,
// tolerating prose or code fences around it.
func extractLines(text string) ([]string, error) {
	if i := strings.Index(text, "["); i >= 0 {
		if j := strings.LastIndex(text, "]"); j > i {
			text = text[i : j+1]
		}
	}
	var lines []string
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		return nil, fmt.Errorf("oneliner: parse lines: %w", err)
	}
	return lines, nil
}

// Gemini calls Google's generateContent API (no SDK dep).
type Gemini struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewGemini(apiKey, model string) *Gemini {
	if model == "" {
		model = "gemini-3.0-flash"
	}
	return &Gemini{APIKey: apiKey, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (g *Gemini) Lines(ctx context.Context, mc MatchContext) ([]string, error) {
	prompt := fmt.Sprintf(
		"Live football match %s vs %s. Live state: %s\nMarket: %%q (%s)\n"+
			"Write exactly 6 one-liners (≤80 chars each) a sharp trader would enjoy — "+
			"witty, specific to the state of this match and this market, no hashtags, no emoji. "+
			"Return them as a JSON array of 6 strings and nothing else.",
		mc.Home, mc.Away, mc.LiveState, mc.MarketRule)
	prompt = fmt.Sprintf(prompt, mc.MarketTitle)

	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
	})
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.Model, g.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oneliner: gemini status %d", resp.StatusCode)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("oneliner: empty gemini response")
	}
	return extractLines(out.Candidates[0].Content.Parts[0].Text)
}

type Service struct {
	store *store.Store
	hub   *ws.Hub
	gen   Generator
	log   *slog.Logger
	Every time.Duration
}

func New(st *store.Store, hub *ws.Hub, gen Generator, log *slog.Logger) *Service {
	return &Service{store: st, hub: hub, gen: gen, log: log, Every: 2 * time.Minute}
}

// GenerateOnce produces and stores one batch for every open market of every
// live match, broadcasting each over WS.
func (s *Service) GenerateOnce(ctx context.Context) error {
	matches, err := s.store.ListMatches(ctx)
	if err != nil {
		return err
	}
	for _, m := range matches {
		if m.Status != "live" {
			continue
		}
		markets, err := s.store.MarketsForMatch(ctx, m.ID)
		if err != nil {
			return err
		}
		for _, mk := range markets {
			if mk.Status != "open" && mk.Status != "closed" {
				continue
			}
			lines, err := s.gen.Lines(ctx, MatchContext{
				Home: m.Home, Away: m.Away, LiveState: string(m.LiveState),
				MarketTitle: mk.Title, MarketRule: mk.Rule,
			})
			if err != nil {
				s.log.Warn("oneliner: generate", "market", mk.Title, "err", err)
				continue
			}
			if err := s.store.InsertOneliners(ctx, mk.MarketID, lines); err != nil {
				return err
			}
			s.hub.Broadcast(ws.Event{
				Type:     ws.EventOneliner,
				MarketID: models.HashString(mk.MarketID),
				Data:     map[string]any{"lines": lines},
			})
		}
	}
	return nil
}

// Run ticks GenerateOnce until ctx cancels.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(s.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.GenerateOnce(ctx); err != nil {
				s.log.Error("oneliner: tick", "err", err)
			}
		}
	}
}
