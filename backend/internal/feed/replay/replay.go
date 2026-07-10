// Package replay implements feed.FeedProvider by reading a recorded fixture's
// events from a local JSON file and emitting them on a compressed timer. It is the
// demo safety net if TxLINE access lags (PROJECT_PLAN.md §9) and lets E2 build the
// full pipeline without waiting on external credentials.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
)

type Provider struct {
	// Dir holds one <fixture_id>.json per recorded match: a JSON array of
	// {event feed.MatchEvent, offset_ms int} in playback order.
	Dir string
	// Speed compresses playback; 60 means 60x real-time (a 90-minute match in ~90s).
	Speed float64
}

func New(dir string, speed float64) *Provider {
	if speed <= 0 {
		speed = 60
	}
	return &Provider{Dir: dir, Speed: speed}
}

type recordedEvent struct {
	Event    feed.MatchEvent `json:"event"`
	OffsetMs int64           `json:"offset_ms"`
}

func (p *Provider) Stream(ctx context.Context, fixtureID string) (<-chan feed.MatchEvent, error) {
	path := filepath.Join(p.Dir, fixtureID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: no recorded fixture %q: %w", fixtureID, err)
	}
	var events []recordedEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, fmt.Errorf("replay: bad fixture %q: %w", fixtureID, err)
	}

	out := make(chan feed.MatchEvent)
	go func() {
		defer close(out)
		start := time.Now()
		for _, re := range events {
			target := start.Add(time.Duration(float64(re.OffsetMs)*float64(time.Millisecond)/p.Speed))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(target)):
			}
			select {
			case out <- re.Event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
