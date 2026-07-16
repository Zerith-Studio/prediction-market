package api_test

// Admin surface: operator-wallet auth round-trip (challenge → sign → session →
// gated call) plus the single-market manual resolve glue. Reuses the shared
// test harness (stack, newWallet, recordingSubmitter) from api_e2e_test.go.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/api"
	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// adminStack mirrors newStack but enables the /admin surface for adminB58.
func adminStack(t *testing.T, adminB58 string) *stack {
	t.Helper()
	log := slog.Default()
	st := storetest.Open(t)
	hub := ws.NewHub(log)
	sub := &recordingSubmitter{}
	ex := exchange.New(st, hub, sub, log)
	ex.SettleSync = true
	rfqSvc := rfq.New(st, hub, nil, log)
	lc := lifecycle.New(st, hub, rfqSvc, nil, nil, log)
	srv := httptest.NewServer(api.New(ex, st, hub, rfqSvc, lc, log).WithAdmin(adminB58).Routes())
	t.Cleanup(srv.Close)
	return &stack{st: st, sub: sub, ex: ex, lc: lc, srv: srv}
}

// adminReq issues a request carrying the admin session token (if non-empty).
func adminReq(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reqBody = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Admin-Session", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func signChallenge(w wallet, message string) string {
	var sig [64]byte
	copy(sig[:], ed25519.Sign(w.priv, []byte(message)))
	return models.SigString(sig)
}

func adminSignIn(t *testing.T, s *stack, w wallet) string {
	t.Helper()
	_, ch := adminReq(t, "GET", s.srv.URL+"/admin/challenge", "", nil)
	message, _ := ch["message"].(string)
	nonce, _ := ch["nonce"].(string)
	_, sess := adminReq(t, "POST", s.srv.URL+"/admin/session", "", map[string]any{
		"pubkey": w.b58, "nonce": nonce, "sig": signChallenge(w, message),
	})
	token, _ := sess["token"].(string)
	if token == "" {
		t.Fatal("admin sign-in failed")
	}
	return token
}

func TestAdminAuthGate(t *testing.T) {
	adminW := newWallet(t)
	stranger := newWallet(t)
	s := adminStack(t, adminW.b58)

	// Gated route with no session → 401.
	if code, _ := adminReq(t, "GET", s.srv.URL+"/admin/markets", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("no-session GET /admin/markets = %d, want 401", code)
	}

	// Challenge.
	code, ch := adminReq(t, "GET", s.srv.URL+"/admin/challenge", "", nil)
	if code != http.StatusOK {
		t.Fatalf("challenge = %d", code)
	}
	nonce, _ := ch["nonce"].(string)
	message, _ := ch["message"].(string)
	if nonce == "" || message == "" {
		t.Fatalf("challenge missing nonce/message: %v", ch)
	}

	// Wrong wallet → 403 (rejected before the nonce is consumed).
	if code, _ := adminReq(t, "POST", s.srv.URL+"/admin/session", "", map[string]any{
		"pubkey": stranger.b58, "nonce": nonce, "sig": signChallenge(stranger, message),
	}); code != http.StatusForbidden {
		t.Fatalf("stranger session = %d, want 403", code)
	}

	// Correct admin wallet → 200 + token (same nonce still valid).
	code, sess := adminReq(t, "POST", s.srv.URL+"/admin/session", "", map[string]any{
		"pubkey": adminW.b58, "nonce": nonce, "sig": signChallenge(adminW, message),
	})
	if code != http.StatusOK {
		t.Fatalf("admin session = %d, want 200 (%v)", code, sess)
	}
	token, _ := sess["token"].(string)
	if token == "" {
		t.Fatalf("no session token: %v", sess)
	}

	// Authed → 200; garbage token → 401; a consumed nonce cannot be replayed.
	if code, _ := adminReq(t, "GET", s.srv.URL+"/admin/markets", token, nil); code != http.StatusOK {
		t.Fatalf("authed GET /admin/markets = %d, want 200", code)
	}
	if code, _ := adminReq(t, "GET", s.srv.URL+"/admin/markets", "deadbeef", nil); code != http.StatusUnauthorized {
		t.Fatalf("garbage token = %d, want 401", code)
	}
	if code, _ := adminReq(t, "POST", s.srv.URL+"/admin/session", "", map[string]any{
		"pubkey": adminW.b58, "nonce": nonce, "sig": signChallenge(adminW, message),
	}); code != http.StatusUnauthorized {
		t.Fatalf("replayed nonce = %d, want 401", code)
	}
}

func TestAdminResolveMarket(t *testing.T) {
	adminW := newWallet(t)
	s := adminStack(t, adminW.b58)
	ctx := context.Background()

	const fixture = "adm-resolve"
	if err := s.lc.RegisterFixture(ctx, fixture, "FRA", "ENG", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	token := adminSignIn(t, s, adminW)

	// Binary market → resolve YES → settled.
	binID := templates.MarketID(fixture, "home_win")
	binHex := models.HashString(binID)
	if code, out := adminReq(t, "POST", s.srv.URL+"/admin/markets/"+binHex+"/resolve", token,
		map[string]any{"outcome": "yes"}); code != http.StatusOK {
		t.Fatalf("resolve binary = %d (%v)", code, out)
	}
	if m, err := s.st.GetMarket(ctx, binID); err != nil || m.Status != "settled" {
		t.Fatalf("binary status = %q err=%v, want settled", m.Status, err)
	}

	// Precision market → settle with a value → settled.
	precID := templates.MarketID(fixture, "precision_total_goals")
	precHex := models.HashString(precID)
	if code, out := adminReq(t, "POST", s.srv.URL+"/admin/markets/"+precHex+"/resolve", token,
		map[string]any{"outcome": "settle", "value": 3.0}); code != http.StatusOK {
		t.Fatalf("resolve precision = %d (%v)", code, out)
	}
	if m, err := s.st.GetMarket(ctx, precID); err != nil || m.Status != "settled" {
		t.Fatalf("precision status = %q err=%v, want settled", m.Status, err)
	}

	// A nonsense binary outcome is a clean 400.
	if code, _ := adminReq(t, "POST", s.srv.URL+"/admin/markets/"+binHex+"/resolve", token,
		map[string]any{"outcome": "banana"}); code != http.StatusBadRequest {
		t.Fatalf("bad outcome should 400, got %d", code)
	}

	// The whole admin surface refuses a request that lost its session mid-flight.
	if code, _ := adminReq(t, "POST", s.srv.URL+"/admin/markets/"+binHex+"/resolve", "",
		map[string]any{"outcome": "yes"}); code != http.StatusUnauthorized {
		t.Fatalf("unauthed resolve = %d, want 401", code)
	}
}
