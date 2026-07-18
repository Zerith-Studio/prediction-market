package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

const (
	maxCommentLen   = 500
	maxCommentBody  = 4096 // request-body cap (bytes)
	commentRateMax  = 5
	commentRateWin  = time.Minute
	maxCommentLinks = 2
)

// bannedWords is a minimal content-filter blocklist (case-insensitive substring).
// Deliberately small/illustrative — extend as needed; a real deployment would use
// a maintained list or a moderation service.
var bannedWords = []string{"nigger", "faggot", "kike", "retard"}

func containsBanned(body string) bool {
	low := strings.ToLower(body)
	for _, w := range bannedWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// validateBody trims + checks a comment body, returning the cleaned body and an
// error message ("" when valid). Shared by post + edit.
func validateBody(raw string) (string, string) {
	body := strings.TrimSpace(raw)
	switch {
	case body == "":
		return "", "comment is empty"
	case utf8.RuneCountInString(body) > maxCommentLen:
		return "", "comment too long (max 500 characters)"
	case strings.Count(strings.ToLower(body), "http") > maxCommentLinks:
		return "", "too many links"
	case containsBanned(body):
		return "", "comment blocked by the content filter"
	}
	return body, ""
}

// commentLimiter is a tiny in-memory per-wallet sliding-window rate limiter for
// comment posts (demo-grade, single-instance; the repo had no limiter before).
type commentLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newCommentLimiter(max int, window time.Duration) *commentLimiter {
	return &commentLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *commentLimiter) allow(wallet string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.window)
	kept := l.hits[wallet][:0]
	for _, t := range l.hits[wallet] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[wallet] = kept
		return false
	}
	l.hits[wallet] = append(kept, time.Now())
	return true
}

var commentRate = newCommentLimiter(commentRateMax, commentRateWin)

type postCommentDTO struct {
	Wallet   string  `json:"wallet"`
	Body     string  `json:"body"`
	ParentID *string `json:"parent_id,omitempty"`
}

// handlePostComment adds an (unsigned, wallet-claimed) comment to a market's
// thread. Hardening: body cap, empty/length/link/profanity checks, per-wallet
// rate limit. Broadcasts a live `comment` event.
func (s *Server) handlePostComment(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCommentBody)
	var d postCommentDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload: "+err.Error())
		return
	}
	if _, err := models.ParsePubkey(d.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "invalid wallet")
		return
	}
	body, verr := validateBody(d.Body)
	if verr != "" {
		httpError(w, http.StatusBadRequest, verr)
		return
	}
	if !commentRate.allow(d.Wallet) {
		httpError(w, http.StatusTooManyRequests, "slow down — too many comments, try again shortly")
		return
	}

	row, err := s.store.InsertComment(r.Context(), m.MarketID, d.ParentID, d.Wallet, body)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusBadRequest, "reply target not found in this market")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(ws.Event{
		Type:     ws.EventComment,
		MarketID: models.HashString(m.MarketID),
		Data:     map[string]any{"action": "new", "comment": row},
	})
	writeJSON(w, http.StatusOK, row)
}

// handleGetComments lists a market's comments (with like counts and, when
// ?wallet= is set, the viewer's liked flags). Degrades to an empty list.
func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	m, ok := s.marketFromPath(w, r)
	if !ok {
		return
	}
	viewer := r.URL.Query().Get("wallet")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.store.ListComments(r.Context(), m.MarketID, viewer, limit)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"comments": []any{}})
		return
	}
	if rows == nil {
		rows = []store.CommentRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": rows})
}

type likeDTO struct {
	Wallet string `json:"wallet"`
}

// handleLikeComment toggles the caller's like on a comment. The `like` event is
// applied by comment_id on the client, so it carries no market routing key.
func (s *Server) handleLikeComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var d likeDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	if _, err := models.ParsePubkey(d.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "invalid wallet")
		return
	}
	liked, count, err := s.store.ToggleLike(r.Context(), id, d.Wallet)
	if err != nil {
		httpError(w, http.StatusBadRequest, "unknown comment")
		return
	}
	s.hub.Broadcast(ws.Event{
		Type: ws.EventComment,
		Data: map[string]any{"action": "like", "comment_id": id, "like_count": count},
	})
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked, "like_count": count})
}

type editCommentDTO struct {
	Wallet string `json:"wallet"`
	Body   string `json:"body"`
}

// handleEditComment lets the (claimed) author edit their own comment body.
func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxCommentBody)
	var d editCommentDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	if _, err := models.ParsePubkey(d.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "invalid wallet")
		return
	}
	body, verr := validateBody(d.Body)
	if verr != "" {
		httpError(w, http.StatusBadRequest, verr)
		return
	}
	marketHex, err := s.store.EditComment(r.Context(), id, d.Wallet, body)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusForbidden, "not your comment")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(ws.Event{
		Type:     ws.EventComment,
		MarketID: marketHex,
		Data:     map[string]any{"action": "edit", "comment_id": id, "body": body},
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "body": body, "edited": true})
}

// handleDeleteOwnComment lets the (claimed) author soft-delete their own comment.
func (s *Server) handleDeleteOwnComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var d likeDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	if _, err := models.ParsePubkey(d.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "invalid wallet")
		return
	}
	marketHex, err := s.store.DeleteOwnComment(r.Context(), id, d.Wallet)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusForbidden, "not your comment")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(ws.Event{
		Type:     ws.EventComment,
		MarketID: marketHex,
		Data:     map[string]any{"action": "delete", "comment_id": id},
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// handleAdminDeleteComment soft-deletes a comment (operator-gated). Replies keep
// their place; the body renders as "[removed]".
func (s *Server) handleAdminDeleteComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	marketHex, err := s.store.SoftDeleteComment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "comment not found")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(ws.Event{
		Type:     ws.EventComment,
		MarketID: marketHex,
		Data:     map[string]any{"action": "delete", "comment_id": id},
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}
