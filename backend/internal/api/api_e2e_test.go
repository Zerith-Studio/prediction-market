package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/api"
	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// recordingSubmitter captures what the crank would send on-chain.
type recordingSubmitter struct {
	mu    sync.Mutex
	fills []matching.Fill
	fail  bool
}

func (r *recordingSubmitter) SettleMatch(_ context.Context, f matching.Fill) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return "", fmt.Errorf("simulated on-chain revert")
	}
	r.fills = append(r.fills, f)
	return fmt.Sprintf("e2e-tx-%d", len(r.fills)), nil
}

type stack struct {
	st  *store.Store
	sub *recordingSubmitter
	ex  *exchange.Exchange
	lc  *lifecycle.Service
	srv *httptest.Server
}

func newStack(t *testing.T) *stack {
	t.Helper()
	log := slog.Default()
	st := storetest.Open(t)
	hub := ws.NewHub(log)
	sub := &recordingSubmitter{}
	ex := exchange.New(st, hub, sub, log)
	ex.SettleSync = true // deterministic assertions
	rfqSvc := rfq.New(st, hub, nil, log)
	lc := lifecycle.New(st, hub, rfqSvc, nil, nil, log)
	srv := httptest.NewServer(api.New(ex, st, hub, rfqSvc, lc, log).Routes())
	t.Cleanup(srv.Close)
	return &stack{st: st, sub: sub, ex: ex, lc: lc, srv: srv}
}

type wallet struct {
	pk   [32]byte
	priv ed25519.PrivateKey
	b58  string
}

func newWallet(t *testing.T) wallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var pk [32]byte
	copy(pk[:], pub)
	return wallet{pk: pk, priv: priv, b58: models.PubkeyString(pk)}
}

func (s *stack) post(t *testing.T, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(s.srv.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func (s *stack) get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(s.srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func signedOrderDTO(w wallet, marketID [32]byte, outcome, side uint8, price uint16, size, salt uint64) map[string]any {
	o := &models.Order{Maker: w.pk, MarketID: marketID, Outcome: outcome, Side: side,
		Price: price, Size: size, Salt: salt}
	models.SignOrder(o, w.priv)
	return map[string]any{
		"maker": w.b58, "market_id": models.HashString(marketID),
		"outcome": outcome, "side": side, "price": price, "size": size,
		"fee_bps": 0, "expiry": 0, "salt": salt, "sig": models.SigString(o.Sig),
	}
}

// The headline test: deposit → signed orders over HTTP → MINT fill → crank
// capture → WS events → Postgres rows → portfolio/settlement reads.
func TestTradeLifecycleOverHTTP(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	const fixture = "api-e2e"
	if err := s.lc.RegisterFixture(ctx, fixture, "ARG", "FRA", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	marketID := templates.MarketID(fixture, "home_win")
	marketHex := models.HashString(marketID)

	// WS client first, so we observe the trade's events.
	wsCtx, wsCancel := context.WithTimeout(ctx, 20*time.Second)
	defer wsCancel()
	conn, _, err := websocket.Dial(wsCtx, "ws"+strings.TrimPrefix(s.srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	alice, bob := newWallet(t), newWallet(t)
	for _, w := range []wallet{alice, bob} {
		resp, _ := s.post(t, "/wallet/deposit", map[string]any{"wallet": w.b58, "amount": 100_000_000})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deposit status %d", resp.StatusCode)
		}
	}

	// Unsigned garbage is rejected before touching any state.
	badOrder := signedOrderDTO(alice, marketID, models.OutcomeYes, models.SideBuy, 65, 40, 1)
	badOrder["sig"] = strings.Repeat("00", 64)
	resp, _ := s.post(t, "/orders", badOrder)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad sig must 401, got %d", resp.StatusCode)
	}

	// Alice BUY YES @65 rests…
	resp, out := s.post(t, "/orders", signedOrderDTO(alice, marketID, models.OutcomeYes, models.SideBuy, 65, 40, 1))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alice order: %d %v", resp.StatusCode, out)
	}
	if fills := out["fills"].([]any); len(fills) != 0 {
		t.Fatalf("alice must rest, got fills %v", fills)
	}
	// …and the book shows her bid.
	code, book := s.get(t, "/markets/"+marketHex+"/book")
	if code != http.StatusOK {
		t.Fatalf("book: %d", code)
	}
	bids := book["bids"].([]any)[models.OutcomeYes].([]any)
	if len(bids) != 1 {
		t.Fatalf("book bids: %v", bids)
	}

	// Bob BUY NO @45 crosses (65+45 ≥ 100) → MINT.
	resp, out = s.post(t, "/orders", signedOrderDTO(bob, marketID, models.OutcomeNo, models.SideBuy, 45, 40, 2))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob order: %d %v", resp.StatusCode, out)
	}
	fills := out["fills"].([]any)
	if len(fills) != 1 || fills[0].(map[string]any)["match_type"] != "MINT" {
		t.Fatalf("want 1 MINT fill, got %v", fills)
	}

	// The crank received exactly that fill, signable and §6.5-buildable.
	s.sub.mu.Lock()
	crankFills := len(s.sub.fills)
	var crankFill matching.Fill
	if crankFills > 0 {
		crankFill = s.sub.fills[0]
	}
	s.sub.mu.Unlock()
	if crankFills != 1 {
		t.Fatalf("crank captured %d fills, want 1", crankFills)
	}
	builder := &crank.TxBuilder{
		ProgramID: solana.MustPublicKeyFromBase58("3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs"),
		USDCMint:  solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"),
	}
	ixs, err := builder.SettleMatchInstructions(crankFill, solana.NewWallet().PublicKey())
	if err != nil || len(ixs) != 3 {
		t.Fatalf("captured fill must build the 3-ix tx: %v", err)
	}

	// WS observed the fill and a book update.
	seen := map[string]bool{}
	deadline := time.Now().Add(10 * time.Second)
	for !(seen[ws.EventFill] && seen[ws.EventBookUpdate]) && time.Now().Before(deadline) {
		var ev ws.Event
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := wsjson.Read(readCtx, conn, &ev)
		cancel()
		if err != nil {
			break
		}
		seen[ev.Type] = true
	}
	if !seen[ws.EventFill] || !seen[ws.EventBookUpdate] {
		t.Errorf("ws events seen: %v (want fill + book_update)", seen)
	}

	// Postgres: fill row exists with the settle tx recorded.
	rows, err := s.st.FillsForMarket(ctx, marketID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fills in store: %v %v", rows, err)
	}
	if rows[0].SettleTx != "e2e-tx-1" {
		t.Errorf("settle_tx = %q, want e2e-tx-1", rows[0].SettleTx)
	}

	// Portfolio: Alice holds 40 YES @65, spent 26 USDC.
	code, pf := s.get(t, "/portfolio?wallet="+alice.b58)
	if code != http.StatusOK {
		t.Fatalf("portfolio: %d", code)
	}
	positions := pf["positions"].([]any)
	if len(positions) != 1 {
		t.Fatalf("positions: %v", positions)
	}
	pos := positions[0].(map[string]any)
	if pos["yes"].(float64) != 40 {
		t.Errorf("alice yes = %v, want 40", pos["yes"])
	}
	bal := pf["balance"].(map[string]any)
	if bal["usdc_available"].(float64) != 74_000_000 {
		t.Errorf("alice available = %v, want 74000000 (100 − 26)", bal["usdc_available"])
	}

	// Replay of the same order → 409.
	resp, _ = s.post(t, "/orders", signedOrderDTO(alice, marketID, models.OutcomeYes, models.SideBuy, 65, 40, 1))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("replay must 409, got %d", resp.StatusCode)
	}

	// Cancel path: rest an order, cancel it, book empties, double-cancel 404s.
	resp, out = s.post(t, "/orders", signedOrderDTO(alice, marketID, models.OutcomeYes, models.SideBuy, 30, 10, 3))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resting order: %d %v", resp.StatusCode, out)
	}
	hash := out["order_hash"].(string)
	req, _ := http.NewRequest(http.MethodDelete, s.srv.URL+"/orders/"+hash+"?maker="+alice.b58, nil)
	dresp, err := http.DefaultClient.Do(req)
	if err != nil || dresp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: %v %d", err, dresp.StatusCode)
	}
	dresp.Body.Close()
	dresp, _ = http.DefaultClient.Do(req)
	if dresp.StatusCode != http.StatusNotFound {
		t.Errorf("double cancel must 404, got %d", dresp.StatusCode)
	}
	dresp.Body.Close()

	// Settlement flow: resolve the fixture, then the settlement endpoint
	// exposes the outcome (2-1 → home_win yes).
	if err := s.lc.ResolveFixture(ctx, fixture, lifecycle.FinalScore{HomeGoals: 2, AwayGoals: 1}); err != nil {
		t.Fatal(err)
	}
	code, settlement := s.get(t, "/markets/"+marketHex+"/settlement")
	if code != http.StatusOK {
		t.Fatalf("settlement: %d", code)
	}
	if settlement["status"] != "settled" {
		t.Errorf("settlement: %v", settlement)
	}
	outcome := settlement["outcome"].(map[string]any)
	if outcome["result"] != "yes" {
		t.Errorf("outcome: %v", outcome)
	}
}

func TestPrecisionAndCombosOverHTTP(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	const fixture = "api-e2e-2"
	if err := s.lc.RegisterFixture(ctx, fixture, "BRA", "GER", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	pool := templates.MarketID(fixture, "precision_total_goals")
	poolHex := models.HashString(pool)

	u := newWallet(t)
	s.post(t, "/wallet/deposit", map[string]any{"wallet": u.b58, "amount": 50_000_000})

	// Enter the pool; re-entry → 409.
	resp, out := s.post(t, "/markets/"+poolHex+"/precision",
		map[string]any{"wallet": u.b58, "guess": 3, "stake": 2_000_000})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("precision entry: %d %v", resp.StatusCode, out)
	}
	resp, _ = s.post(t, "/markets/"+poolHex+"/precision",
		map[string]any{"wallet": u.b58, "guess": 4, "stake": 1_000_000})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second entry must 409, got %d", resp.StatusCode)
	}
	code, lb := s.get(t, "/markets/"+poolHex+"/precision/leaderboard")
	if code != http.StatusOK || len(lb["entries"].([]any)) != 1 {
		t.Fatalf("leaderboard: %d %v", code, lb)
	}

	// Combo over HTTP: create RFQ → MM posts a signed quote → taker accepts.
	mm := newWallet(t)
	s.post(t, "/wallet/deposit", map[string]any{"wallet": mm.b58, "amount": 100_000_000})
	legs := []map[string]any{
		{"market_id": models.HashString(templates.MarketID(fixture, "home_win")), "outcome": 1},
		{"market_id": models.HashString(templates.MarketID(fixture, "over_2_5")), "outcome": 1},
	}
	resp, out = s.post(t, "/combos", map[string]any{"taker": u.b58, "legs": legs, "stake": 5_000_000})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create rfq: %d %v", resp.StatusCode, out)
	}
	rfqID := out["rfq_id"].(string)

	q := &models.ComboQuote{
		Maker: mm.pk,
		Legs: []models.Leg{
			{MarketID: templates.MarketID(fixture, "home_win"), Outcome: 1},
			{MarketID: templates.MarketID(fixture, "over_2_5"), Outcome: 1},
		},
		Stake: 5_000_000, Payout: 18_000_000,
		Expiry: time.Now().Add(time.Minute).Unix(), Salt: 7,
	}
	models.SignQuote(q, mm.priv)
	resp, out = s.post(t, "/combos/"+rfqID+"/quotes", map[string]any{
		"maker": mm.b58, "legs": legs, "stake": q.Stake, "payout": q.Payout,
		"expiry": q.Expiry, "salt": q.Salt, "sig": models.SigString(q.Sig),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit quote: %d %v", resp.StatusCode, out)
	}
	quoteHash := out["quote_hash"].(string)

	resp, out = s.post(t, "/combos/"+rfqID+"/accept",
		map[string]any{"quote_hash": quoteHash, "taker": u.b58})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept: %d %v", resp.StatusCode, out)
	}
	// Double-accept → 409 (single-use quote).
	resp, _ = s.post(t, "/combos/"+rfqID+"/accept",
		map[string]any{"quote_hash": quoteHash, "taker": u.b58})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("double accept must 409, got %d", resp.StatusCode)
	}

	code, combo := s.get(t, "/combos/"+rfqID)
	if code != http.StatusOK {
		t.Fatalf("get rfq: %d", code)
	}
	quotes := combo["quotes"].([]any)
	if len(quotes) != 1 || quotes[0].(map[string]any)["status"] != "accepted" {
		t.Errorf("quotes: %v", quotes)
	}
}

// A settle revert must be a no-op for the maker: mirror rolled back, book
// restored, order fillable again (interface-contract §6.2).
func TestRevertReconcilesEverywhere(t *testing.T) {
	s := newStack(t)
	s.sub.fail = true
	ctx := context.Background()
	const fixture = "api-e2e-3"
	if err := s.lc.RegisterFixture(ctx, fixture, "ESP", "POR", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	marketID := templates.MarketID(fixture, "home_win")

	alice, bob := newWallet(t), newWallet(t)
	for _, w := range []wallet{alice, bob} {
		s.post(t, "/wallet/deposit", map[string]any{"wallet": w.b58, "amount": 100_000_000})
	}

	s.post(t, "/orders", signedOrderDTO(alice, marketID, models.OutcomeYes, models.SideBuy, 65, 40, 1))
	resp, out := s.post(t, "/orders", signedOrderDTO(bob, marketID, models.OutcomeNo, models.SideBuy, 45, 40, 2))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob: %d %v", resp.StatusCode, out)
	}

	// Settlement reverted → balances fully restored, no positions, fill gone.
	code, pf := s.get(t, "/portfolio?wallet="+alice.b58)
	if code != http.StatusOK {
		t.Fatal("portfolio")
	}
	bal := pf["balance"].(map[string]any)
	if bal["usdc_available"].(float64) != 74_000_000 || bal["usdc_locked"].(float64) != 26_000_000 {
		t.Errorf("alice after revert: %v (want lock restored, nothing spent)", bal)
	}
	fills, _ := s.st.FillsForMarket(ctx, marketID, 10)
	if len(fills) != 0 {
		t.Errorf("fill rows must be deleted on revert: %v", fills)
	}
	// The restored orders can cross again (once the submitter recovers).
	s.sub.fail = false
	carol := newWallet(t)
	s.post(t, "/wallet/deposit", map[string]any{"wallet": carol.b58, "amount": 100_000_000})
	resp, out = s.post(t, "/orders", signedOrderDTO(carol, marketID, models.OutcomeNo, models.SideBuy, 45, 40, 3))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("carol: %d %v", resp.StatusCode, out)
	}
	if fills := out["fills"].([]any); len(fills) != 1 {
		t.Fatalf("restored book must fill carol: %v", out)
	}
}

