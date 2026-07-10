// Package api implements the E2 off-chain REST/WS surface (interface-contract.md §5).
package api

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

// Server holds the in-memory book registry. TODO: persist orders/fills to Postgres
// (internal/db, schema in db/schema.sql) — books here are the live matching state,
// Postgres is the durable index per PROJECT_PLAN.md §4.
type Server struct {
	mu    sync.RWMutex
	books map[[32]byte]*matching.Book
	log   *slog.Logger
}

func New(log *slog.Logger) *Server {
	return &Server{books: make(map[[32]byte]*matching.Book), log: log}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", s.handlePostOrder)
	mux.HandleFunc("GET /markets/{id}/book", s.handleGetBook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (s *Server) bookFor(marketID [32]byte) *matching.Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.books[marketID]
	if !ok {
		b = matching.NewBook(marketID)
		s.books[marketID] = b
	}
	return b
}

// handlePostOrder accepts a signed Order (interface-contract.md §1), submits it to
// the market's book, and returns any fills produced.
// TODO: verify ed25519 sig here before admitting to the book (defense in depth —
// E1 re-verifies on-chain regardless, per docs/adr/0003), and reject SELL orders
// the maker doesn't hold tokens for (interface-contract.md §1 "enforced at E2 entry").
func (s *Server) handlePostOrder(w http.ResponseWriter, r *http.Request) {
	var o models.Order
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		http.Error(w, "bad order payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	fills := s.bookFor(o.MarketID).Submit(&o)
	hash := models.OrderHash(&o)

	resp := struct {
		OrderHash string          `json:"order_hash"`
		Fills     []matching.Fill `json:"fills"`
	}{OrderHash: hex.EncodeToString(hash[:]), Fills: fills}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("encode response", "err", err)
	}
}

// handleGetBook is a placeholder — TODO: return aggregated price levels once the
// Book exposes a read view (currently write-only via Submit).
func (s *Server) handleGetBook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"todo":"aggregate book levels"}`))
}
