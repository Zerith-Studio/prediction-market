// Package api implements the E2 off-chain REST surface (interface-contract.md §5,
// PROJECT_PLAN §6) over the exchange core + services. Wire format: pubkeys are
// base58, hashes/market ids are 64-char hex, signatures 128-char hex, money is
// integer micro-USDC, prices integer cents 1..99.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/feed/txodds"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

// FixtureSource exposes the TxLINE feed's on-demand reads to the admin panel
// (fixture discovery + odds snapshot). *txodds.Provider implements it; nil in
// replay / off-chain mode disables the admin fixture browser.
type FixtureSource interface {
	Fixtures(ctx context.Context, competitionID int) ([]txodds.Fixture, error)
	OddsSnapshot(ctx context.Context, fixtureID string) (map[string]uint16, error)
}

type Server struct {
	ex        *exchange.Exchange
	store     *store.Store
	hub       *ws.Hub
	rfq       *rfq.Service
	lifecycle *lifecycle.Service
	chain     *crank.ChainOps        // nil = off-chain mirror mode
	fixtures  FixtureSource          // nil = admin fixture browser disabled
	admin     *adminAuth             // operator-wallet gate for /admin (nil until WithAdmin)
	pricer    lifecycle.FairPriceSink // MM bot's fair-price sink (admin manual pricing); nil = disabled
	log       *slog.Logger
}

func New(ex *exchange.Exchange, st *store.Store, hub *ws.Hub, rfqSvc *rfq.Service,
	lc *lifecycle.Service, log *slog.Logger) *Server {
	return &Server{ex: ex, store: st, hub: hub, rfq: rfqSvc, lifecycle: lc, log: log}
}

// WithChain enables the real on-chain deposit flow.
func (s *Server) WithChain(c *crank.ChainOps) *Server {
	s.chain = c
	return s
}

// WithFixtures wires the live feed's on-demand reads into the admin panel.
func (s *Server) WithFixtures(src FixtureSource) *Server {
	s.fixtures = src
	return s
}

// WithAdmin enables the /admin surface, gated by operator-wallet signatures from
// adminPubkeyB58 (base58). An empty pubkey leaves /admin returning 503.
func (s *Server) WithAdmin(adminPubkeyB58 string) *Server {
	s.admin = newAdminAuth(adminPubkeyB58)
	return s
}

// WithPricer lets the admin panel push a manual fair price to the MM bot (which
// then quotes that market two-sided and can answer combos on it). nil disables.
func (s *Server) WithPricer(p lifecycle.FairPriceSink) *Server {
	s.pricer = p
	return s
}

// WithCORS wraps the mux for browser clients (the Next.js frontend). Demo
// posture: reflect any origin unless CORS_ORIGIN pins one. No credentials are
// used — auth is the ed25519 signature inside the order payload itself.
func WithCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := origin
		if allow == "" || allow == "*" {
			allow = r.Header.Get("Origin")
			if allow == "" {
				allow = "*"
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", allow)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Session")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Trading
	mux.HandleFunc("POST /orders", s.handlePostOrder)
	mux.HandleFunc("DELETE /orders/{hash}", s.handleCancelOrder)

	// Markets & matches
	mux.HandleFunc("GET /matches", s.handleListMatches)
	mux.HandleFunc("GET /matches/{id}", s.handleGetMatch)
	mux.HandleFunc("GET /news", s.handleGetBreakingNews)
	mux.HandleFunc("GET /markets", s.handleListMarkets)
	mux.HandleFunc("GET /markets/{id}", s.handleGetMarket)
	mux.HandleFunc("GET /markets/{id}/book", s.handleGetBook)
	mux.HandleFunc("GET /markets/{id}/fills", s.handleGetFills)
	mux.HandleFunc("GET /markets/{id}/settlement", s.handleGetSettlement)
	mux.HandleFunc("GET /markets/{id}/oneliners", s.handleGetOneliners)

	// Comments (per-market threads; unsigned wallet-claim)
	mux.HandleFunc("GET /markets/{id}/comments", s.handleGetComments)
	mux.HandleFunc("POST /markets/{id}/comments", s.handlePostComment)
	mux.HandleFunc("POST /comments/{id}/like", s.handleLikeComment)
	mux.HandleFunc("POST /comments/{id}/edit", s.handleEditComment)
	mux.HandleFunc("POST /comments/{id}/delete", s.handleDeleteOwnComment)

	// Combos (RFQ)
	mux.HandleFunc("POST /combos", s.handleCreateRFQ)
	mux.HandleFunc("GET /combos", s.handleListRFQs)
	mux.HandleFunc("GET /combos/{id}", s.handleGetRFQ)
	mux.HandleFunc("POST /combos/{id}/quotes", s.handleSubmitQuote)
	mux.HandleFunc("POST /combos/{id}/accept", s.handleAcceptQuote)

	// Precision pools
	mux.HandleFunc("POST /markets/{id}/precision", s.handleEnterPrecision)
	mux.HandleFunc("GET /markets/{id}/precision/leaderboard", s.handleLeaderboard)

	// Wallet / portfolio
	mux.HandleFunc("POST /wallet/deposit", s.handleDeposit)
	mux.HandleFunc("POST /wallet/deposit-init", s.handleDepositInit)
	mux.HandleFunc("POST /wallet/deposit-complete", s.handleDepositComplete)
	mux.HandleFunc("GET /balance", s.handleBalance)
	mux.HandleFunc("GET /portfolio", s.handlePortfolio)

	// Watchlist
	mux.HandleFunc("GET /watchlist", s.handleGetWatchlist)
	mux.HandleFunc("POST /watchlist", s.handleAddWatch)
	mux.HandleFunc("DELETE /watchlist/{market_id}", s.handleRemoveWatch)

	// Admin — operator-gated manual market control (auth in admin.go)
	mux.HandleFunc("GET /admin/challenge", s.handleAdminChallenge)
	mux.HandleFunc("POST /admin/session", s.handleAdminSession)
	mux.HandleFunc("GET /admin/fixtures", s.adminGuard(s.handleAdminFixtures))
	mux.HandleFunc("GET /admin/fixtures/{id}/odds", s.adminGuard(s.handleAdminFixtureOdds))
	mux.HandleFunc("POST /admin/fixtures/{id}/markets", s.adminGuard(s.handleAdminCreateMarkets))
	mux.HandleFunc("POST /admin/fixtures/{id}/resolve", s.adminGuard(s.handleAdminResolveFixture))
	mux.HandleFunc("GET /admin/markets", s.adminGuard(s.handleAdminMarkets))
	mux.HandleFunc("POST /admin/markets/{id}/resolve", s.adminGuard(s.handleAdminResolveMarket))
	mux.HandleFunc("POST /admin/markets/{id}/close", s.adminGuard(s.handleAdminCloseMarket))
	mux.HandleFunc("POST /admin/markets/{id}/cancel-orders", s.adminGuard(s.handleAdminCancelOrders))
	mux.HandleFunc("POST /admin/markets/{id}/price", s.adminGuard(s.handleAdminSetPrice))
	mux.HandleFunc("POST /admin/markets/{id}/pin", s.adminGuard(s.handleAdminPinMarket))
	mux.HandleFunc("DELETE /admin/comments/{id}", s.adminGuard(s.handleAdminDeleteComment))
	mux.HandleFunc("GET /admin/ops", s.adminGuard(s.handleAdminOps))

	// Ops
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /ws", s.hub.Handler())

	return mux
}

// ---- wire DTOs ----

type orderDTO struct {
	Maker    string `json:"maker"`     // base58
	MarketID string `json:"market_id"` // hex
	Outcome  uint8  `json:"outcome"`
	Side     uint8  `json:"side"`
	Price    uint16 `json:"price"`
	Size     uint64 `json:"size"`
	FeeBps   uint16 `json:"fee_bps"`
	Expiry   int64  `json:"expiry"`
	Salt     uint64 `json:"salt"`
	Sig      string `json:"sig"` // hex(128)
}

func (d orderDTO) toModel() (*models.Order, error) {
	maker, err := models.ParsePubkey(d.Maker)
	if err != nil {
		return nil, err
	}
	marketID, err := models.ParseHash(d.MarketID)
	if err != nil {
		return nil, err
	}
	sig, err := models.ParseSig(d.Sig)
	if err != nil {
		return nil, err
	}
	return &models.Order{
		Maker: maker, MarketID: marketID, Outcome: d.Outcome, Side: d.Side,
		Price: d.Price, Size: d.Size, FeeBps: d.FeeBps, Expiry: d.Expiry,
		Salt: d.Salt, Sig: sig,
	}, nil
}

type fillDTO struct {
	TakerHash string `json:"taker_hash"`
	MakerHash string `json:"maker_hash"`
	Price     uint16 `json:"price"`
	Size      uint64 `json:"size"`
	MatchType string `json:"match_type"`
}

func fillDTOs(fills []matching.Fill) []fillDTO {
	out := make([]fillDTO, len(fills))
	for i, f := range fills {
		mt := "NORMAL"
		switch f.MatchType {
		case models.MatchMint:
			mt = "MINT"
		case models.MatchMerge:
			mt = "MERGE"
		}
		out[i] = fillDTO{
			TakerHash: models.HashString(f.Taker.Hash),
			MakerHash: models.HashString(f.Maker.Hash),
			Price:     f.Price, Size: f.Size, MatchType: mt,
		}
	}
	return out
}

// ---- handlers ----

func (s *Server) handlePostOrder(w http.ResponseWriter, r *http.Request) {
	var d orderDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad order payload: "+err.Error())
		return
	}
	o, err := d.toModel()
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, fills, err := s.ex.SubmitOrder(r.Context(), o)
	if err != nil {
		httpError(w, orderErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"order_hash": models.HashString(hash),
		"fills":      fillDTOs(fills),
	})
}

func orderErrStatus(err error) int {
	switch {
	case errors.Is(err, exchange.ErrBadSignature):
		return http.StatusUnauthorized
	case errors.Is(err, store.ErrInsufficientFunds),
		errors.Is(err, store.ErrInsufficientTokens):
		return http.StatusPaymentRequired
	case errors.Is(err, store.ErrDuplicateOrder),
		errors.Is(err, matching.ErrDuplicate):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	hash, err := models.ParseHash(r.PathValue("hash"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Maker proves identity by base58 pubkey (demo: no session auth — the
	// on-chain cancel_order is the real authorization, maker-signed).
	maker, err := models.ParsePubkey(r.URL.Query().Get("maker"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "maker query param required: "+err.Error())
		return
	}
	if err := s.ex.Cancel(r.Context(), hash, maker); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no live order with that hash for this maker")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": models.HashString(hash)})
}

func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.ListMatches(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": marshalMatches(matches)})
}

func marshalMatches(matches []store.MatchRow) []map[string]any {
	out := make([]map[string]any, len(matches))
	for i, m := range matches {
		out[i] = matchJSON(m, false)
	}
	return out
}

// matchJSON builds a match DTO. The list view stays lean; the detail view
// (withLineups) also carries the team sheets, which can be large.
func matchJSON(m store.MatchRow, withLineups bool) map[string]any {
	out := map[string]any{
		"id": m.ID, "fixture_id": m.FixtureID, "home": m.Home, "away": m.Away,
		"kickoff_at": m.KickoffAt, "status": m.Status,
		"live_state": json.RawMessage(m.LiveState),
	}
	if withLineups {
		out["lineups"] = json.RawMessage(m.Lineups) // 'null' when the feed hasn't sent them
	}
	return out
}

// handleGetMatch returns one match's full detail (live_state + team sheets) by
// its UUID id — the /market page's match-centre source.
func (s *Server) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatchByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, matchJSON(m, true))
}

// handleGetBreakingNews returns the latest hourly breaking-news batch (real Exa
// articles + real Yes%/delta) for the markets index. Degrades to an empty list
// rather than erroring — the panel simply hides when there's nothing fresh.
func (s *Server) handleGetBreakingNews(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.LatestBreakingNews(r.Context(), 6*time.Hour, 12)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	if items == nil {
		items = []store.NewsRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func marketJSON(m store.MarketRow) map[string]any {
	out := map[string]any{
		"id": m.ID, "market_id": models.HashString(m.MarketID), "match_id": m.MatchID,
		"template_key": m.TemplateKey, "type": m.Type, "title": m.Title,
		"rule": m.Rule, "status": m.Status, "created_at": m.CreatedAt,
	}
	if len(m.Outcome) > 0 && string(m.Outcome) != "null" {
		out["outcome"] = json.RawMessage(m.Outcome)
	}
	if m.ChainTx != "" {
		out["chain_tx"] = m.ChainTx
	}
	if m.FeaturedRank != nil {
		out["featured_rank"] = *m.FeaturedRank
	}
	return out
}

func (s *Server) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	markets, err := s.store.ListMarkets(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, len(markets))
	for i, m := range markets {
		out[i] = marketJSON(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"markets": out})
}

func (s *Server) marketFromPath(w http.ResponseWriter, r *http.Request) (store.MarketRow, bool) {
	id, err := models.ParseHash(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return store.MarketRow{}, false
	}
	m, err := s.store.GetMarket(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "unknown market")
		return m, false
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return m, false
	}
	return m, true
}

func (s *Server) handleGetMarket(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, marketJSON(m))
}

func (s *Server) handleGetBook(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.ex.Book(m.MarketID))
}

func (s *Server) handleGetFills(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	fills, err := s.store.FillsForMarket(r.Context(), m.MarketID, 100)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fills": fills})
}

// handleGetSettlement powers /settlement/[id]: the resolved outcome plus the
// on-chain tx for the "Verified on Solana ↗" link.
func (s *Server) handleGetSettlement(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	resp := map[string]any{
		"market_id": models.HashString(m.MarketID),
		"title":     m.Title,
		"status":    m.Status,
	}
	if len(m.Outcome) > 0 && string(m.Outcome) != "null" {
		resp["outcome"] = json.RawMessage(m.Outcome)
	}
	if m.ChainTx != "" {
		resp["chain_tx"] = m.ChainTx
		resp["explorer_url"] = "https://explorer.solana.com/tx/" + m.ChainTx + "?cluster=devnet"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetOneliners(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	lines, at, err := s.store.LatestOneliners(r.Context(), m.MarketID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "generated_at": at})
}

// ---- combos ----

type legDTO struct {
	MarketID string `json:"market_id"`
	Outcome  uint8  `json:"outcome"`
}

func parseLegs(in []legDTO) ([]models.Leg, error) {
	legs := make([]models.Leg, len(in))
	for i, l := range in {
		id, err := models.ParseHash(l.MarketID)
		if err != nil {
			return nil, err
		}
		legs[i] = models.Leg{MarketID: id, Outcome: l.Outcome}
	}
	return legs, nil
}

func (s *Server) handleCreateRFQ(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Taker string   `json:"taker"`
		Legs  []legDTO `json:"legs"`
		Stake uint64   `json:"stake"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := models.ParsePubkey(req.Taker); err != nil {
		httpError(w, http.StatusBadRequest, "taker: "+err.Error())
		return
	}
	legs, err := parseLegs(req.Legs)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.rfq.CreateRFQ(r.Context(), req.Taker, legs, req.Stake)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rfq_id": id})
}

// handleListRFQs lists open RFQs awaiting a quote — the market-maker view
// (a human MM answers these through the same POST /combos/{id}/quotes the bot uses).
func (s *Server) handleListRFQs(w http.ResponseWriter, r *http.Request) {
	rfqs, err := s.store.OpenRFQs(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, len(rfqs))
	for i, rq := range rfqs {
		legs := make([]map[string]any, len(rq.Legs))
		for j, l := range rq.Legs {
			legs[j] = map[string]any{"market_id": models.HashString(l.MarketID), "outcome": l.Outcome}
		}
		out[i] = map[string]any{
			"id": rq.ID, "taker": rq.Taker, "stake": rq.Stake,
			"legs": legs, "status": rq.Status, "created_at": rq.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rfqs": out})
}

func (s *Server) handleGetRFQ(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.store.GetRFQ(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "unknown rfq")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	quotes, err := s.store.QuotesForRFQ(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	qs := make([]map[string]any, len(quotes))
	for i, q := range quotes {
		qs[i] = map[string]any{
			"quote_hash": models.HashString(q.QuoteHash),
			"maker":      q.Maker, "stake": q.Stake, "payout": q.Payout,
			"expiry": q.Expiry, "status": q.Status,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rfq":    req,
		"quotes": qs,
	})
}

// handleSubmitQuote lets an MM answer an RFQ (the bot posts through the same
// path a third-party MM would).
func (s *Server) handleSubmitQuote(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Maker  string   `json:"maker"`
		Legs   []legDTO `json:"legs"`
		Stake  uint64   `json:"stake"`
		Payout uint64   `json:"payout"`
		Expiry int64    `json:"expiry"`
		Salt   uint64   `json:"salt"`
		Sig    string   `json:"sig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	maker, err := models.ParsePubkey(d.Maker)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	legs, err := parseLegs(d.Legs)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	sig, err := models.ParseSig(d.Sig)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	q := &models.ComboQuote{Maker: maker, Legs: legs, Stake: d.Stake,
		Payout: d.Payout, Expiry: d.Expiry, Salt: d.Salt, Sig: sig}
	if err := s.rfq.SubmitQuote(r.Context(), q, r.PathValue("id")); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"quote_hash": models.HashString(models.QuoteHash(q)),
	})
}

func (s *Server) handleAcceptQuote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuoteHash string `json:"quote_hash"`
		Taker     string `json:"taker"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := models.ParseHash(req.QuoteHash)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	taker, err := models.ParsePubkey(req.Taker)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	txSig, err := s.rfq.Accept(r.Context(), hash, taker)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrQuoteNotOpen) {
			status = http.StatusConflict
		}
		if errors.Is(err, store.ErrInsufficientFunds) {
			status = http.StatusPaymentRequired
		}
		httpError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": req.QuoteHash, "accept_tx": txSig})
}

// ---- precision ----

func (s *Server) handleEnterPrecision(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Wallet string  `json:"wallet"`
		Guess  float64 `json:"guess"`
		Stake  uint64  `json:"stake"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := models.ParsePubkey(req.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet: "+err.Error())
		return
	}
	id, err := s.store.EnterPrecision(r.Context(), m.MarketID, req.Wallet, req.Guess, req.Stake)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, store.ErrAlreadyEntered):
			status = http.StatusConflict
		case errors.Is(err, store.ErrInsufficientFunds):
			status = http.StatusPaymentRequired
		case errors.Is(err, store.ErrMarketNotOpen):
			status = http.StatusGone // kickoff-lock: entries closed
		}
		httpError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry_id": id})
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	entries, err := s.store.PrecisionLeaderboard(r.Context(), m.MarketID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "status": m.Status})
}

// ---- wallet / portfolio ----

// handleDepositInit starts the REAL deposit: the server builds an
// operator-cosigned tx (SOL top-up, USDC mint-to, init_vault, deposit) and the
// wallet signs the returned message bytes — the product's one signing popup.
func (s *Server) handleDepositInit(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		httpError(w, http.StatusConflict, "server is in off-chain mirror mode — use POST /wallet/deposit")
		return
	}
	var req struct {
		Wallet string `json:"wallet"`
		Amount uint64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	pk, err := models.ParsePubkey(req.Wallet)
	if err != nil || req.Amount == 0 {
		httpError(w, http.StatusBadRequest, "wallet (base58) and amount (micro-USDC) required")
		return
	}
	id, msgB64, err := s.chain.PrepareDeposit(r.Context(), solana.PublicKeyFromBytes(pk[:]), req.Amount)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deposit_id": id, "message_b64": msgB64})
}

func (s *Server) handleDepositComplete(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		httpError(w, http.StatusConflict, "server is in off-chain mirror mode")
		return
	}
	var req struct {
		DepositID string `json:"deposit_id"`
		Wallet    string `json:"wallet"`
		Amount    uint64 `json:"amount"`
		Sig       string `json:"sig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	sig, err := models.ParseSig(req.Sig)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	txSig, err := s.chain.CompleteDeposit(r.Context(), req.DepositID, sig)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Mirror the confirmed on-chain deposit into the soft-lock ledger.
	b, err := s.store.Deposit(r.Context(), req.Wallet, req.Amount)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tx": txSig, "balance": b})
}

// handleDeposit mirrors an on-chain vault deposit into the demo ledger.
func (s *Server) handleDeposit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Wallet string `json:"wallet"`
		Amount uint64 `json:"amount"` // micro-USDC
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := models.ParsePubkey(req.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet: "+err.Error())
		return
	}
	if req.Amount == 0 {
		httpError(w, http.StatusBadRequest, "amount must be > 0")
		return
	}
	b, err := s.store.Deposit(r.Context(), req.Wallet, req.Amount)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	b, err := s.store.GetBalance(r.Context(), wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handlePortfolio: the three /portfolio sections (positions, open orders,
// history) plus combos and precision entries.
func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	ctx := r.Context()

	balance, err := s.store.GetBalance(ctx, wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	positions, err := s.store.GetPositions(ctx, wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	posOut := make([]map[string]any, len(positions))
	for i, p := range positions {
		// Mark for unrealized PnL: the best price the position could exit at
		// NOW (unified YES ladder best bid — BBP).
		book := s.ex.Book(p.MarketID)
		bestBid := 0
		if len(book.Bids[1]) > 0 {
			bestBid = int(book.Bids[1][0].Price)
		}
		if len(book.Asks[0]) > 0 && 100-int(book.Asks[0][0].Price) > bestBid {
			bestBid = 100 - int(book.Asks[0][0].Price)
		}
		posOut[i] = map[string]any{
			"market_id": models.HashString(p.MarketID),
			"yes":       p.Yes, "no": p.No,
			"yes_locked": p.YesLocked, "no_locked": p.NoLocked,
			"avg_cost": p.AvgCost,
			"realized": p.Realized,
			"best_bid": bestBid,
		}
	}
	orders, err := s.store.OrdersByMaker(ctx, wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ordOut := make([]map[string]any, len(orders))
	for i, o := range orders {
		var expiry any
		if o.Order.Expiry != 0 {
			expiry = time.Unix(o.Order.Expiry, 0).UTC()
		}
		ordOut[i] = map[string]any{
			"order_hash": models.HashString(o.OrderHash),
			"market_id":  models.HashString(o.Order.MarketID),
			"outcome":    o.Order.Outcome, "side": o.Order.Side,
			"price": o.Order.Price, "size": o.Order.Size,
			"remaining": o.Remaining, "status": o.Status, "expiry": expiry,
		}
	}
	fills, err := s.store.FillsForWallet(ctx, wallet, 100)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	escrows, err := s.store.EscrowsForWallet(ctx, wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	escOut := make([]map[string]any, len(escrows))
	for i, e := range escrows {
		legDetails := make([]map[string]any, len(e.LegDetails))
		for j, l := range e.LegDetails {
			legDetails[j] = map[string]any{
				"market_id": models.HashString(l.MarketID), "outcome": l.Outcome,
			}
		}
		escOut[i] = map[string]any{
			"quote_hash": models.HashString(e.QuoteHash),
			"taker":      e.Taker, "status": e.Status,
			"stake": e.Stake, "payout": e.Payout, "legs": e.Legs,
			"leg_details": legDetails,
			"accept_tx":   e.AcceptTx, "resolve_tx": e.ResolveTx,
		}
	}

	precEntries, err := s.store.PrecisionEntriesForWallet(ctx, wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	precOut := make([]map[string]any, len(precEntries))
	for i, p := range precEntries {
		row := map[string]any{
			"market_id": models.HashString(p.MarketID),
			"title":     p.Title, "status": p.Status,
			"guess": p.Guess, "stake": p.Stake, "ts": p.TS,
		}
		if p.Score != nil {
			row["score"] = *p.Score
		}
		if p.Payout != nil {
			row["payout"] = *p.Payout
		}
		precOut[i] = row
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"balance":   balance,
		"positions": posOut,
		"orders":    ordOut,
		"fills":     fills,
		"combos":    escOut,
		"precision": precOut,
	})
}

// handleGetWatchlist returns a wallet's watched market ids (64-hex), newest first.
func (s *Server) handleGetWatchlist(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	ids, err := s.store.Watchlist(r.Context(), wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = models.HashString(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"market_ids": out})
}

// handleAddWatch favourites a market for a wallet. Body: {wallet, market_id}.
func (s *Server) handleAddWatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Wallet   string `json:"wallet"`
		MarketID string `json:"market_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if _, err := models.ParsePubkey(body.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet: "+err.Error())
		return
	}
	marketID, err := models.ParseHash(body.MarketID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "market_id: "+err.Error())
		return
	}
	if err := s.store.AddWatch(r.Context(), body.Wallet, marketID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusBadRequest, "unknown market")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRemoveWatch unfavourites a market. Path: market_id; query: wallet.
func (s *Server) handleRemoveWatch(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	marketID, err := models.ParseHash(r.PathValue("market_id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "market_id: "+err.Error())
		return
	}
	if err := s.store.RemoveWatch(r.Context(), wallet, marketID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
