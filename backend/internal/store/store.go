// Package store is the Postgres layer for the off-chain index (PROJECT_PLAN.md §4):
// the durable order book mirror, soft-locks, RFQ, precision pools, one-liners, and a
// read-cache of chain state. The chain is authoritative for money/positions/settlement
// (interface-contract.md §6.2) — everything here is UX state that must be reconcilable.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zerith-Studio/prediction-market/backend/db"
)

var (
	ErrInsufficientFunds  = errors.New("store: insufficient available USDC")
	ErrInsufficientTokens = errors.New("store: insufficient outcome tokens")
	ErrDuplicateOrder     = errors.New("store: order hash already exists")
	ErrNotFound           = errors.New("store: not found")
	ErrAlreadyEntered     = errors.New("store: one precision entry per wallet per pool")
	ErrQuoteNotOpen       = errors.New("store: combo quote is not open")
	ErrMarketNotOpen      = errors.New("store: market is not open")
)

type Store struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres. Simple query protocol keeps us compatible with
// transaction-pooling proxies (Neon's pgbouncer endpoint), which break named
// prepared statements.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("store: parse DATABASE_URL: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Bootstrap applies db/schema.sql (idempotent — everything is IF NOT EXISTS).
func (s *Store) Bootstrap(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, db.Schema); err != nil {
		return fmt.Errorf("store: bootstrap schema: %w", err)
	}
	return nil
}

func (s *Store) Close() { s.pool.Close() }

// tx runs fn inside a transaction, committing on nil and rolling back on error.
func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	txn, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer txn.Rollback(ctx)
	if err := fn(txn); err != nil {
		return err
	}
	return txn.Commit(ctx)
}
