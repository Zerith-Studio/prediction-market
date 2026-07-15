package ws

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func testHub(t *testing.T) (*Hub, string) {
	t.Helper()
	hub := NewHub(slog.Default())
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func waitClients(t *testing.T, hub *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for hub.ClientCount() != n {
		if time.Now().After(deadline) {
			t.Fatalf("want %d clients, have %d", n, hub.ClientCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBroadcastReachesAllClients(t *testing.T) {
	hub, url := testHub(t)
	c1 := dial(t, url)
	c2 := dial(t, url)
	waitClients(t, hub, 2)

	hub.Broadcast(Event{Type: EventBookUpdate, MarketID: "aa", Data: map[string]any{"x": 1}})

	for _, c := range []*websocket.Conn{c1, c2} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var got Event
		err := wsjson.Read(ctx, c, &got)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got.Type != EventBookUpdate || got.MarketID != "aa" || got.TS.IsZero() {
			t.Errorf("event: %+v", got)
		}
	}
}

func TestDisconnectRemovesClient(t *testing.T) {
	hub, url := testHub(t)
	c := dial(t, url)
	waitClients(t, hub, 1)
	c.Close(websocket.StatusNormalClosure, "")
	waitClients(t, hub, 0)
	// Broadcast to an empty hub must not panic or block.
	hub.Broadcast(Event{Type: EventFill, Data: nil})
}

func TestSlowClientDoesNotBlockBroadcast(t *testing.T) {
	hub, url := testHub(t)
	dial(t, url) // never reads
	waitClients(t, hub, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ { // > channel buffer of 256
			hub.Broadcast(Event{Type: EventOneliner, Data: i})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast blocked on a slow client")
	}
}
