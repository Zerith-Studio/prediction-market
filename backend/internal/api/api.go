// Package api implements the E2 off-chain REST surface (interface-contract.md §5,
// PROJECT_PLAN §6) over the exchange core + services. Wire format: pubkeys are
// base58, hashes/market ids are 64-char hex, signatures 128-char hex, money is
// integer micro-USDC, prices integer cents 1..99.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

type Server struct {
	ex        *exchange.Exchange
	store     *store.Store
	hub       *ws.Hub
	rfq       *rfq.Service
	lifecycle *lifecycle.Service
	log       *slog.Logger
}

func New(ex *exchange.Exchange, st *store.Store, hub *ws.Hub, rfqSvc *rfq.Service,
	lc *lifecycle.Service, log *slog.Logger) *Server {
	return &Server{ex: ex, store: st, hub: hub, rfq: rfqSvc, lifecycle: lc, log: log}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Trading
	mux.HandleFunc("POST /orders", s.handlePostOrder)
	mux.HandleFunc("DELETE /orders/{hash}", s.handleCancelOrder)

	// Markets & matches
	mux.HandleFunc("GET /matches", s.handleListMatches)
	mux.HandleFunc("GET /markets", s.handleListMarkets)
	mux.HandleFunc("GET /markets/{id}", s.handleGetMarket)
	mux.HandleFunc("GET /markets/{id}/book", s.handleGetBook)
	mux.HandleFunc("GET /markets/{id}/fills", s.handleGetFills)
	mux.HandleFunc("GET /markets/{id}/settlement", s.handleGetSettlement)
	mux.HandleFunc("GET /markets/{id}/oneliners", s.handleGetOneliners)

	// Combos (RFQ)
	mux.HandleFunc("POST /combos", s.handleCreateRFQ)
	mux.HandleFunc("GET /combos/{id}", s.handleGetRFQ)
	mux.HandleFunc("POST /combos/{id}/quotes", s.handleSubmitQuote)
	mux.HandleFunc("POST /combos/{id}/accept", s.handleAcceptQuote)

	// Precision pools
	mux.HandleFunc("POST /markets/{id}/precision", s.handleEnterPrecision)
	mux.HandleFunc("GET /markets/{id}/precision/leaderboard", s.handleLeaderboard)

	// Wallet / portfolio
	mux.HandleFunc("POST /wallet/deposit", s.handleDeposit)
	mux.HandleFunc("GET /balance", s.handleBalance)
	mux.HandleFunc("GET /portfolio", s.handlePortfolio)

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
		out[i] = map[string]any{
			"id": m.ID, "fixture_id": m.FixtureID, "home": m.Home, "away": m.Away,
			"kickoff_at": m.KickoffAt, "status": m.Status,
			"live_state": json.RawMessage(m.LiveState),
		}
	}
	return out
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
		posOut[i] = map[string]any{
			"market_id": models.HashString(p.MarketID),
			"yes":       p.Yes, "no": p.No,
			"yes_locked": p.YesLocked, "no_locked": p.NoLocked,
			"avg_cost": p.AvgCost,
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
		escOut[i] = map[string]any{
			"quote_hash": models.HashString(e.QuoteHash),
			"taker":      e.Taker, "status": e.Status,
			"accept_tx": e.AcceptTx, "resolve_tx": e.ResolveTx,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"balance":   balance,
		"positions": posOut,
		"orders":    ordOut,
		"fills":     fills,
		"combos":    escOut,
	})
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
