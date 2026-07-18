package api_test

// Whole-server load test: many wallets trade concurrently over real HTTP
// against a live-configured stack (real Postgres, real matching, real API +
// WS hub). The demo won't push this volume, but if the server survives a
// hostile flood it will survive a demo. Asserts: zero 5xx responses, share
// conservation (buyers hold exactly what the seller sold), and the WS hub
// keeps delivering under load.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// rawPost is goroutine-safe (no *testing.T): returns status + decoded body.
func rawPost(baseURL, path string, body any) (int, map[string]any) {
	raw, _ := json.Marshal(body)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestConcurrentHTTPLoad(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	const fixture = "load"
	if err := s.lc.RegisterFixture(ctx, fixture, "LDA", "LDB", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	market := templates.MarketID(fixture, "home_win")
	marketHex := models.HashString(market)

	// Deep resting SELL liquidity: 1000 YES @55 from one seller.
	seller := newWallet(t)
	s.st.GrantTokens(ctx, seller.b58, market, 1000, 0)
	if _, _, _, err := s.ex.SubmitOrder(ctx, signedModelOrder(seller, market, models.OutcomeYes, models.SideSell, 55, 1000, 1)); err != nil {
		t.Fatal(err)
	}

	// A WS subscriber that must keep receiving events through the flood.
	wsCtx, wsCancel := context.WithTimeout(ctx, 40*time.Second)
	defer wsCancel()
	conn, _, err := websocket.Dial(wsCtx, "ws"+strings.TrimPrefix(s.srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	var wsEvents atomic.Int64
	go func() {
		for {
			var ev ws.Event
			if err := wsjson.Read(wsCtx, conn, &ev); err != nil {
				return
			}
			wsEvents.Add(1)
		}
	}()

	// 40 buyers, each: deposit then fire a forged order (must not 5xx) and a
	// good order that should fill. Good demand = 40×10 = 400 vs 1000 resting.
	const buyers = 40
	var got5xx atomic.Int64
	var goodFills atomic.Uint64
	var wg sync.WaitGroup
	gate := make(chan struct{})

	for i := 0; i < buyers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := newWallet(t)
			<-gate

			if code, _ := rawPost(s.srv.URL, "/wallet/deposit",
				map[string]any{"wallet": w.b58, "amount": 100_000_000}); code >= 500 {
				got5xx.Add(1)
			}

			forged := signedOrderDTO(w, market, models.OutcomeYes, models.SideBuy, 60, 10, uint64(9_000_000+i))
			forged["sig"] = strings.Repeat("00", 64)
			if code, _ := rawPost(s.srv.URL, "/orders", forged); code >= 500 {
				got5xx.Add(1)
			}

			good := signedOrderDTO(w, market, models.OutcomeYes, models.SideBuy, 60, 10, uint64(1_000_000+i))
			code, out := rawPost(s.srv.URL, "/orders", good)
			if code >= 500 {
				got5xx.Add(1)
			} else if code == http.StatusOK {
				if fills, ok := out["fills"].([]any); ok {
					goodFills.Add(uint64(len(fills)) * 10)
				}
			}
		}(i)
	}
	close(gate)
	wg.Wait()

	if n := got5xx.Load(); n != 0 {
		t.Fatalf("%d requests returned 5xx under load", n)
	}

	// Share conservation: what buyers filled equals what the seller sold.
	filled := goodFills.Load()
	if filled == 0 || filled > 1000 {
		t.Fatalf("filled %d shares — want 1..1000", filled)
	}
	if code, _ := s.get(t, "/markets/"+marketHex+"/book"); code >= 500 {
		t.Fatalf("book read 5xx after load")
	}
	sp, _ := s.st.GetPositions(ctx, seller.b58)
	soldByBook := uint64(1000)
	if len(sp) == 1 {
		soldByBook = 1000 - sp[0].Yes
	}
	if soldByBook != filled {
		t.Errorf("seller sold %d but buyers filled %d — share leak", soldByBook, filled)
	}

	// The WS hub delivered events throughout (book_update/fill per trade).
	deadline := time.Now().Add(3 * time.Second)
	for wsEvents.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if wsEvents.Load() == 0 {
		t.Error("WS hub delivered no events during the load — clients would see a frozen book")
	}
}
