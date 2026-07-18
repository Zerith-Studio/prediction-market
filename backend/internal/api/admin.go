// Admin surface: operator-gated manual control of the market lifecycle —
// browse TxLINE fixtures + odds, create a fixture's markets on demand, and
// resolve/void markets (per-market or whole-fixture-from-score) with real
// on-chain settlement. It exists so we don't depend on the live feed's timing
// to demo resolution (the full-time cascade can stay quiet for hours).
//
// Auth is an operator-wallet signature (no passwords, no sessions server-side
// beyond an in-memory token): GET /admin/challenge → sign the message with the
// admin wallet → POST /admin/session → carry X-Admin-Session on every call.
// This is demo-grade (tokens live in memory); production auth is out of scope.
package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

const (
	adminChallengePrefix = "pitchmarket-admin:" // domain separator for the signed message
	adminNonceTTL        = 2 * time.Minute
	adminSessionTTL      = 8 * time.Hour
	adminSessionHeader   = "X-Admin-Session"
)

// adminAuth holds the single authorized pubkey plus the in-memory challenge and
// session stores. All fields are guarded by mu.
type adminAuth struct {
	pubkey  [32]byte
	enabled bool

	mu       sync.Mutex
	nonces   map[string]time.Time // challenge nonce → expiry
	sessions map[string]time.Time // session token → expiry
}

func newAdminAuth(pubkeyB58 string) *adminAuth {
	a := &adminAuth{
		nonces:   make(map[string]time.Time),
		sessions: make(map[string]time.Time),
	}
	if pubkeyB58 != "" {
		if pk, err := models.ParsePubkey(pubkeyB58); err == nil {
			a.pubkey = pk
			a.enabled = true
		}
	}
	return a
}

func randomHex() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// issueNonce mints a short-lived challenge, pruning expired ones opportunistically.
func (a *adminAuth) issueNonce() (string, time.Time, error) {
	nonce, err := randomHex()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	exp := now.Add(adminNonceTTL)
	a.mu.Lock()
	for n, e := range a.nonces {
		if now.After(e) {
			delete(a.nonces, n)
		}
	}
	a.nonces[nonce] = exp
	a.mu.Unlock()
	return nonce, exp, nil
}

// consumeNonce removes the nonce and reports whether it was known and unexpired.
func (a *adminAuth) consumeNonce(nonce string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.nonces[nonce]
	if !ok {
		return false
	}
	delete(a.nonces, nonce)
	return time.Now().Before(exp)
}

func (a *adminAuth) issueSession() (string, time.Time, error) {
	token, err := randomHex()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(adminSessionTTL)
	a.mu.Lock()
	a.sessions[token] = exp
	a.mu.Unlock()
	return token, exp, nil
}

func (a *adminAuth) validSession(token string) bool {
	if token == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(a.sessions, token)
		return false
	}
	return true
}

// adminGuard wraps a handler with the operator-session check.
func (s *Server) adminGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.admin == nil || !s.admin.enabled {
			httpError(w, http.StatusServiceUnavailable, "admin surface disabled (set ADMIN_PUBKEY)")
			return
		}
		if !s.admin.validSession(r.Header.Get(adminSessionHeader)) {
			httpError(w, http.StatusUnauthorized, "admin session required — sign in via /admin/session")
			return
		}
		next(w, r)
	}
}

// ---- auth handlers ----

func (s *Server) handleAdminChallenge(w http.ResponseWriter, r *http.Request) {
	if s.admin == nil || !s.admin.enabled {
		httpError(w, http.StatusServiceUnavailable, "admin surface disabled (set ADMIN_PUBKEY)")
		return
	}
	nonce, exp, err := s.admin.issueNonce()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce":   nonce,
		"message": adminChallengePrefix + nonce, // the exact UTF-8 string the wallet signs
		"expires": exp,
	})
}

type adminSessionDTO struct {
	Pubkey string `json:"pubkey"` // base58
	Nonce  string `json:"nonce"`
	Sig    string `json:"sig"` // hex(128) over []byte(message)
}

func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	if s.admin == nil || !s.admin.enabled {
		httpError(w, http.StatusServiceUnavailable, "admin surface disabled (set ADMIN_PUBKEY)")
		return
	}
	var d adminSessionDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad session payload: "+err.Error())
		return
	}
	pk, err := models.ParsePubkey(d.Pubkey)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad pubkey: "+err.Error())
		return
	}
	if pk != s.admin.pubkey {
		httpError(w, http.StatusForbidden, "not the admin wallet")
		return
	}
	sig, err := models.ParseSig(d.Sig)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad signature: "+err.Error())
		return
	}
	// Consume the nonce first so a bad signature still burns the challenge.
	if !s.admin.consumeNonce(d.Nonce) {
		httpError(w, http.StatusUnauthorized, "unknown or expired challenge")
		return
	}
	msg := []byte(adminChallengePrefix + d.Nonce)
	if !ed25519.Verify(ed25519.PublicKey(pk[:]), msg, sig[:]) {
		httpError(w, http.StatusUnauthorized, "signature does not verify")
		return
	}
	token, exp, err := s.admin.issueSession()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires": exp})
}

// ---- fixtures & odds ----

func (s *Server) handleAdminFixtures(w http.ResponseWriter, r *http.Request) {
	if s.fixtures == nil {
		httpError(w, http.StatusServiceUnavailable, "live fixture feed not configured")
		return
	}
	comp := 72 // World Cup
	if v := r.URL.Query().Get("competition"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			comp = n
		}
	}
	fixtures, err := s.fixtures.Fixtures(r.Context(), comp)
	if err != nil {
		httpError(w, http.StatusBadGateway, "txodds fixtures: "+err.Error())
		return
	}
	// Flag fixtures whose markets are already registered.
	registered := map[string]bool{}
	if matches, err := s.store.ListMatches(r.Context()); err == nil {
		for _, m := range matches {
			registered[m.FixtureID] = true
		}
	}
	out := make([]map[string]any, len(fixtures))
	for i, f := range fixtures {
		id := strconv.FormatInt(f.ID, 10)
		out[i] = map[string]any{
			"id": id, "home": f.Home, "away": f.Away,
			"kickoff": f.Kickoff, "competition": f.Competition,
			"live": f.Live, "registered": registered[id],
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fixtures": out})
}

func (s *Server) handleAdminFixtureOdds(w http.ResponseWriter, r *http.Request) {
	if s.fixtures == nil {
		httpError(w, http.StatusServiceUnavailable, "live fixture feed not configured")
		return
	}
	id := r.PathValue("id")
	odds, err := s.fixtures.OddsSnapshot(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusBadGateway, "txodds odds: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fixture_id": id, "odds": odds})
}

// ---- market creation ----

type adminCreateMarketsDTO struct {
	Home    string    `json:"home"`
	Away    string    `json:"away"`
	Kickoff time.Time `json:"kickoff"` // RFC3339; zero → now+2h fallback
}

func (s *Server) handleAdminCreateMarkets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var d adminCreateMarketsDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	if d.Home == "" || d.Away == "" {
		httpError(w, http.StatusBadRequest, "home and away are required")
		return
	}
	kickoff := d.Kickoff
	if kickoff.IsZero() {
		kickoff = time.Now().Add(2 * time.Hour)
	}
	if err := s.lifecycle.RegisterFixture(r.Context(), id, d.Home, d.Away, kickoff); err != nil {
		httpError(w, http.StatusInternalServerError, "register fixture: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fixture_id": id,
		"markets":    s.marketsForFixture(r.Context(), id),
	})
}

// ---- markets & resolution ----

func (s *Server) handleAdminMarkets(w http.ResponseWriter, r *http.Request) {
	markets, err := s.store.ListMarkets(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, len(markets))
	for i, m := range markets {
		j := marketJSON(m)
		if m.Type == "binary" {
			j["book"] = bookSummary(s.ex.Book(m.MarketID))
		}
		out[i] = j
	}
	writeJSON(w, http.StatusOK, map[string]any{"markets": out})
}

// bookSummary is a compact top-of-book view (YES ladder) for the admin table.
func bookSummary(snap matching.Snapshot) map[string]any {
	yesBids, yesAsks := snap.Bids[models.OutcomeYes], snap.Asks[models.OutcomeYes]
	sum := map[string]any{"bid_levels": len(yesBids), "ask_levels": len(yesAsks)}
	if len(yesBids) > 0 {
		sum["yes_bid"] = yesBids[0].Price
	}
	if len(yesAsks) > 0 {
		sum["yes_ask"] = yesAsks[0].Price
	}
	return sum
}

type adminResolveDTO struct {
	Outcome  string          `json:"outcome"` // binary: yes|no|void. precision: "void" refunds; else settle
	Value    *float64        `json:"value"`   // precision actual (required unless void)
	Evidence json.RawMessage `json:"evidence"`
}

type adminCreateCustomMarketDTO struct {
	Scope            string          `json:"scope"`
	FixtureID        string          `json:"fixture_id"`
	Home             string          `json:"home"`
	Away             string          `json:"away"`
	Kickoff          time.Time       `json:"kickoff"`
	TemplateKey      string          `json:"template_key"`
	Type             string          `json:"type"`
	Title            string          `json:"title"`
	Rule             string          `json:"rule"`
	CompetitionID    string          `json:"competition_id"`
	SubjectType      string          `json:"subject_type"`
	SubjectID        string          `json:"subject_id"`
	ResolutionSource string          `json:"resolution_source"`
	RuleJSON         json.RawMessage `json:"rule_json"`
}

func (s *Server) handleAdminCreateCustomMarket(w http.ResponseWriter, r *http.Request) {
	var d adminCreateCustomMarketDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	if d.TemplateKey == "" || d.Title == "" || d.Rule == "" {
		httpError(w, http.StatusBadRequest, "template_key, title, and rule are required")
		return
	}
	var matchID string
	if d.Scope == "fixture" || d.FixtureID != "" {
		if d.FixtureID == "" || d.Home == "" || d.Away == "" {
			httpError(w, http.StatusBadRequest, "fixture custom markets require fixture_id, home, and away")
			return
		}
		kickoff := d.Kickoff
		if kickoff.IsZero() {
			kickoff = time.Now().Add(2 * time.Hour)
		}
		var err error
		matchID, err = s.store.UpsertMatch(r.Context(), d.FixtureID, d.Home, d.Away, kickoff)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "upsert fixture: "+err.Error())
			return
		}
		if d.Scope == "" {
			d.Scope = "fixture"
		}
	}
	req := store.CustomMarketRequest{
		Scope:            d.Scope,
		FixtureID:        d.FixtureID,
		MatchID:          matchID,
		TemplateKey:      d.TemplateKey,
		Type:             d.Type,
		Title:            d.Title,
		Rule:             d.Rule,
		CompetitionID:    d.CompetitionID,
		SubjectType:      d.SubjectType,
		SubjectID:        d.SubjectID,
		ResolutionSource: d.ResolutionSource,
		RuleJSON:         d.RuleJSON,
	}
	marketID, err := s.store.CreateCustomMarket(r.Context(), req)
	if err != nil {
		httpError(w, http.StatusBadRequest, "create custom market: "+err.Error())
		return
	}
	if s.chain != nil && (d.Type == "" || d.Type == "binary") {
		if _, err := s.chain.InitializeMarket(r.Context(), marketID); err != nil {
			s.log.Error("admin: custom on-chain initialize_market failed — market stays mirror-only",
				"market", models.HashString(marketID), "err", err)
		}
	}
	m, err := s.store.GetMarket(r.Context(), marketID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, marketJSON(m))
}

func (s *Server) handleAdminResolveMarket(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	var d adminResolveDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	txSig, err := s.lifecycle.ResolveMarketManually(r.Context(), m.MarketID, d.Outcome, d.Value)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	evidence := d.Evidence
	if len(evidence) == 0 {
		evidence = json.RawMessage(`{"manual":true}`)
	}
	actor := ""
	if s.admin != nil {
		actor = solana.PublicKeyFromBytes(s.admin.pubkey[:]).String()
	}
	if actor == "" {
		actor = "admin"
	}
	if err := s.store.RecordResolutionAttempt(r.Context(), store.ResolutionAttempt{
		MarketID: m.MarketID,
		Actor:    actor,
		Outcome:  d.Outcome,
		Evidence: evidence,
		Tx:       txSig,
	}); err != nil {
		httpError(w, http.StatusInternalServerError, "record resolution evidence: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"market_id": models.HashString(m.MarketID),
		"tx":        txSig,
	})
}

func (s *Server) handleAdminCloseMarket(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	if err := s.store.SetMarketStatus(r.Context(), m.MarketID, "closed"); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"market_id": models.HashString(m.MarketID), "status": "closed",
	})
}

// handleAdminCancelOrders clears a market's resting book (off-chain cancel of
// every live order) — the clean-slate button that used to require raw SQL.
func (s *Server) handleAdminCancelOrders(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	rows, err := s.store.LiveOrders(r.Context(), m.MarketID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cancelled := 0
	for _, row := range rows {
		if err := s.ex.Cancel(r.Context(), row.OrderHash, row.Order.Maker); err != nil {
			s.log.Warn("admin: cancel order", "hash", models.HashString(row.OrderHash), "err", err)
			continue
		}
		cancelled++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"market_id": models.HashString(m.MarketID), "cancelled": cancelled,
	})
}

type adminPriceDTO struct {
	Price uint16 `json:"price"` // YES fair price, cents 1..99
}

// handleAdminSetPrice pushes a manual fair price to the MM bot for a binary
// market: the bot then quotes it two-sided (seeding liquidity to trade against)
// and can answer combo RFQs that include this market. Handy when TxLINE isn't
// pricing a fixture (manually-created markets, or a fixture not yet live).
func (s *Server) handleAdminSetPrice(w http.ResponseWriter, r *http.Request) {
	if s.pricer == nil {
		httpError(w, http.StatusServiceUnavailable, "MM bot not running — cannot set price")
		return
	}
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	if m.Type != "binary" {
		httpError(w, http.StatusBadRequest, "fair price applies to binary markets only")
		return
	}
	var d adminPriceDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	if d.Price < 1 || d.Price > 99 {
		httpError(w, http.StatusBadRequest, "price must be 1..99 cents")
		return
	}
	s.pricer.OnFairPrice(m.MarketID, d.Price)
	writeJSON(w, http.StatusOK, map[string]any{
		"market_id": models.HashString(m.MarketID), "price": d.Price,
	})
}

type adminPinDTO struct {
	// Pinned toggles featured state; Rank optionally sets an explicit order
	// (defaults to 100 when pinning without one). Ignored when Pinned is false.
	Pinned bool `json:"pinned"`
	Rank   *int `json:"rank,omitempty"`
}

// handleAdminPinMarket pins/unpins a market for the featured hero on the markets
// index (featured_rank NULL = unpinned). Lower rank = higher priority.
func (s *Server) handleAdminPinMarket(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	var d adminPinDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	var rank *int
	if d.Pinned {
		rank = d.Rank
		if rank == nil {
			def := 100
			rank = &def
		}
	}
	if err := s.store.SetMarketFeatured(r.Context(), m.MarketID, rank); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"market_id": models.HashString(m.MarketID), "featured_rank": rank,
	})
}

// handleAdminResolveFixture fires the full cascade from a final score: every
// binary market resolves on-chain, precision pools settle, combos sweep.
func (s *Server) handleAdminResolveFixture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var final lifecycle.FinalScore
	if err := json.NewDecoder(r.Body).Decode(&final); err != nil {
		httpError(w, http.StatusBadRequest, "bad score payload: "+err.Error())
		return
	}
	if err := s.lifecycle.ResolveFixture(r.Context(), id, final); err != nil {
		httpError(w, http.StatusInternalServerError, "resolve fixture: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fixture_id": id,
		"markets":    s.marketsForFixture(r.Context(), id), // now carry chain_tx + outcome
	})
}

// ---- ops dashboard ----

func (s *Server) handleAdminOps(w http.ResponseWriter, r *http.Request) {
	ops := map[string]any{"chain_enabled": s.chain != nil}
	if s.admin != nil {
		ops["admin_pubkey"] = solana.PublicKeyFromBytes(s.admin.pubkey[:]).String()
	}
	if s.chain != nil {
		operator := s.chain.Operator.PublicKey()
		ops["operator"] = operator.String()
		if bal, err := s.chain.Client.GetBalance(r.Context(), operator, rpc.CommitmentConfirmed); err == nil {
			ops["operator_sol"] = float64(bal.Value) / 1e9
		}
	}
	// TxLINE credential status (optional capability on the fixture source).
	if cs, ok := s.fixtures.(interface{ CredentialsExpiry() time.Time }); ok {
		if exp := cs.CredentialsExpiry(); !exp.IsZero() {
			ops["txline_expires"] = exp
			ops["txline_valid"] = time.Now().Before(exp)
		}
	}
	// Market status tallies.
	tally := map[string]int{}
	if markets, err := s.store.ListMarkets(r.Context(), ""); err == nil {
		for _, m := range markets {
			tally[m.Status]++
		}
	}
	ops["markets_by_status"] = tally

	// Stale unresolved matches — well past kickoff but still not fully settled.
	// The reconciler auto-resolves what it can; whatever lingers here needs the
	// operator to resolve it by hand (the hybrid policy's manual-review backstop).
	stale := []map[string]any{}
	if rows, err := s.store.UnresolvedMatches(r.Context(), time.Now().Add(-3*time.Hour)); err == nil {
		for _, m := range rows {
			stale = append(stale, map[string]any{
				"fixture_id": m.FixtureID, "home": m.Home, "away": m.Away,
				"kickoff": m.KickoffAt, "status": m.Status,
			})
		}
	}
	ops["stale_matches"] = stale

	writeJSON(w, http.StatusOK, ops)
}

// marketsForFixture returns a fixture's markets as JSON (best-effort; empty on
// any lookup error).
func (s *Server) marketsForFixture(ctx context.Context, fixtureID string) []map[string]any {
	m, err := s.store.GetMatchByFixture(ctx, fixtureID)
	if err != nil {
		return []map[string]any{}
	}
	rows, err := s.store.MarketsForMatch(ctx, m.ID)
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, len(rows))
	for i, mr := range rows {
		out[i] = marketJSON(mr)
	}
	return out
}
