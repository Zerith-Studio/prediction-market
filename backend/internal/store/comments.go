package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// CommentRow is one comment served to the market page. wallet is the base58
// poster (client-claimed — comments are unsigned, unlike orders).
type CommentRow struct {
	ID         string    `json:"id"`
	MarketID   string    `json:"market_id"` // 64-hex
	ParentID   *string   `json:"parent_id,omitempty"`
	Wallet     string    `json:"wallet"`
	AvatarSeed string    `json:"avatar_seed"` // users.avatar_seed, else the wallet
	Body       string    `json:"body"`        // "" when deleted
	Deleted    bool      `json:"deleted"`
	Edited     bool      `json:"edited"`
	LikeCount  int       `json:"like_count"`
	Liked      bool      `json:"liked"` // whether the viewer wallet liked it
	CreatedAt  time.Time `json:"created_at"`
}

// InsertComment adds a comment (parentID nil = top-level) and returns the row. A
// reply's parent must belong to the same market, else ErrNotFound.
func (s *Store) InsertComment(ctx context.Context, marketID [32]byte, parentID *string, wallet, body string) (CommentRow, error) {
	var c CommentRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO comments (market_id, parent_id, wallet, body)
		SELECT $1, $2::uuid, $3, $4
		WHERE $2::uuid IS NULL
		   OR EXISTS (SELECT 1 FROM comments p WHERE p.id = $2::uuid AND p.market_id = $1)
		RETURNING id, encode(market_id,'hex'), parent_id, wallet,
		          COALESCE((SELECT avatar_seed FROM users u WHERE u.wallet = comments.wallet), comments.wallet),
		          body, false, false, 0, false, created_at`,
		marketID[:], parentID, wallet, body).
		Scan(&c.ID, &c.MarketID, &c.ParentID, &c.Wallet, &c.AvatarSeed, &c.Body,
			&c.Deleted, &c.Edited, &c.LikeCount, &c.Liked, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound // parent not in this market
	}
	return c, err
}

// ListComments returns a market's comments (deleted rows blanked, marked
// deleted), with like counts and — when viewer is set — whether the viewer liked
// each. Ordered oldest-first; the client nests replies by parent_id.
func (s *Store) ListComments(ctx context.Context, marketID [32]byte, viewer string, limit int) ([]CommentRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, encode(c.market_id,'hex'), c.parent_id, c.wallet,
		       COALESCE(u.avatar_seed, c.wallet),
		       CASE WHEN c.deleted_at IS NULL THEN c.body ELSE '' END,
		       (c.deleted_at IS NOT NULL),
		       (c.edited_at IS NOT NULL),
		       COALESCE(lc.cnt, 0),
		       ($2 <> '' AND vl.wallet IS NOT NULL),
		       c.created_at
		FROM comments c
		LEFT JOIN users u ON u.wallet = c.wallet
		LEFT JOIN (SELECT comment_id, count(*) AS cnt FROM comment_likes GROUP BY comment_id) lc
		       ON lc.comment_id = c.id
		LEFT JOIN comment_likes vl ON vl.comment_id = c.id AND vl.wallet = $2
		WHERE c.market_id = $1
		ORDER BY c.created_at
		LIMIT $3`, marketID[:], viewer, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommentRow
	for rows.Next() {
		var c CommentRow
		if err := rows.Scan(&c.ID, &c.MarketID, &c.ParentID, &c.Wallet, &c.AvatarSeed, &c.Body,
			&c.Deleted, &c.Edited, &c.LikeCount, &c.Liked, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ToggleLike flips a wallet's like on a comment and returns the new state +
// count. A like on a non-existent comment fails the FK (surfaced as an error).
func (s *Store) ToggleLike(ctx context.Context, commentID, wallet string) (liked bool, count int, err error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM comment_likes WHERE comment_id = $1::uuid AND wallet = $2`, commentID, wallet)
	if err != nil {
		return false, 0, err
	}
	if tag.RowsAffected() > 0 {
		liked = false
	} else {
		if _, err = s.pool.Exec(ctx,
			`INSERT INTO comment_likes (comment_id, wallet) VALUES ($1::uuid, $2)
			 ON CONFLICT (comment_id, wallet) DO NOTHING`, commentID, wallet); err != nil {
			return false, 0, err
		}
		liked = true
	}
	err = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM comment_likes WHERE comment_id = $1::uuid`, commentID).Scan(&count)
	return liked, count, err
}

// SoftDeleteComment marks a comment deleted (keeps its replies) and returns the
// owning market (hex) for the WS broadcast. ErrNotFound if missing/already gone.
func (s *Store) SoftDeleteComment(ctx context.Context, commentID string) (string, error) {
	var marketHex string
	err := s.pool.QueryRow(ctx, `
		UPDATE comments SET deleted_at = now()
		WHERE id = $1::uuid AND deleted_at IS NULL
		RETURNING encode(market_id,'hex')`, commentID).Scan(&marketHex)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return marketHex, err
}

// EditComment updates the author's own comment body and stamps edited_at,
// returning the owning market (hex). Only the claimed author can edit a live
// comment; anything else → ErrNotFound.
func (s *Store) EditComment(ctx context.Context, commentID, wallet, body string) (string, error) {
	var marketHex string
	err := s.pool.QueryRow(ctx, `
		UPDATE comments SET body = $3, edited_at = now()
		WHERE id = $1::uuid AND wallet = $2 AND deleted_at IS NULL
		RETURNING encode(market_id,'hex')`, commentID, wallet, body).Scan(&marketHex)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return marketHex, err
}

// DeleteOwnComment soft-deletes the author's own comment (self-service, distinct
// from admin moderation), returning the owning market (hex).
func (s *Store) DeleteOwnComment(ctx context.Context, commentID, wallet string) (string, error) {
	var marketHex string
	err := s.pool.QueryRow(ctx, `
		UPDATE comments SET deleted_at = now()
		WHERE id = $1::uuid AND wallet = $2 AND deleted_at IS NULL
		RETURNING encode(market_id,'hex')`, commentID, wallet).Scan(&marketHex)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return marketHex, err
}

// RecentCommentCount counts a wallet's comments since `since` — the rate-limit
// backstop behind the in-memory limiter.
func (s *Store) RecentCommentCount(ctx context.Context, wallet string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM comments WHERE wallet = $1 AND created_at > $2`, wallet, since).Scan(&n)
	return n, err
}
