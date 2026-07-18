// Package ws is the /ws broadcast hub (interface-contract.md §5). Every event
// carries a type from the pinned set — book_update, fill, order_update,
// combo_quote, match_state, oneliner, and comment (added for live comment
// threads) — plus routing keys; clients filter client-side (demo scale: one
// match, a handful of markets).
package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// The WS event types. The first six are the original frozen surface; comment was
// added deliberately for live comment threads (a conscious contract expansion).
const (
	EventBookUpdate = "book_update"
	EventFill       = "fill"
	EventOrder      = "order_update"
	EventComboQuote = "combo_quote"
	EventMatchState = "match_state"
	EventOneliner   = "oneliner"
	EventComment    = "comment"
)

type Event struct {
	Type      string    `json:"type"`
	MarketID  string    `json:"market_id,omitempty"`  // hex, when market-scoped
	FixtureID string    `json:"fixture_id,omitempty"` // when match-scoped
	Data      any       `json:"data"`
	TS        time.Time `json:"ts"`
}

type client struct {
	send chan Event
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	log     *slog.Logger
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{clients: make(map[*client]struct{}), log: log}
}

// Broadcast fans the event out to every connected client. Slow consumers drop
// events rather than blocking the trading path — the book snapshot is always
// re-fetchable over REST, WS is a hint stream, not a source of truth.
func (h *Hub) Broadcast(ev Event) {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- ev:
		default:
			h.log.Warn("ws: dropping event for slow client", "type", ev.Type)
		}
	}
}

// ClientCount reports connected clients (health/debug).
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Handler upgrades GET /ws and streams broadcast events until the client goes away.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"}, // demo: frontend origin varies (localhost/Vercel)
		})
		if err != nil {
			h.log.Warn("ws: accept", "err", err)
			return
		}

		c := &client{send: make(chan Event, 256)}
		h.mu.Lock()
		h.clients[c] = struct{}{}
		h.mu.Unlock()
		defer func() {
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			conn.Close(websocket.StatusNormalClosure, "bye")
		}()

		ctx := r.Context()
		// Reader: we never expect client messages; this drains pings/closes and
		// unblocks when the client disconnects.
		readErr := make(chan struct{})
		go func() {
			defer close(readErr)
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-readErr:
				return
			case ev := <-c.send:
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := wsjson.Write(writeCtx, conn, ev)
				cancel()
				if err != nil {
					return
				}
			}
		}
	})
}
