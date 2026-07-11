// Package txodds implements feed.FeedProvider against the live TxODDS/TxLINE
// stream. SKELETON: the exact endpoint shapes are pending TxODDS's reply to
// docs/txodds-day1-email.md — the SSE framing below follows their public docs'
// conventions and normalizes into feed.MatchEvent so nothing downstream changes
// when credentials land. Until then the replay provider is the demo path
// (PROJECT_PLAN §9 safety net).
package txodds

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
)

var ErrNotConfigured = errors.New("txodds: TXODDS_URL / TXODDS_API_KEY not set")

type Provider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func New(baseURL, apiKey string) (*Provider, error) {
	if baseURL == "" || apiKey == "" {
		return nil, ErrNotConfigured
	}
	return &Provider{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey,
		Client: &http.Client{}}, nil
}

// Stream subscribes to the fixture's SSE event stream and normalizes each
// `data:` frame into a feed.MatchEvent. Signed frames carry the TxLINE
// signature through SignedProof (oracle tier d, ADR 0005).
func (p *Provider) Stream(ctx context.Context, fixtureID string) (<-chan feed.MatchEvent, error) {
	url := fmt.Sprintf("%s/v1/fixtures/%s/events", p.BaseURL, fixtureID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("txodds: connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("txodds: stream status %d", resp.StatusCode)
	}

	out := make(chan feed.MatchEvent)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			payload, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue // comments, event:, id:, blank keep-alives
			}
			var ev feed.MatchEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &ev); err != nil {
				continue // skip malformed frames, keep the stream alive
			}
			if ev.FixtureID == "" {
				ev.FixtureID = fixtureID
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
