// Package store is the data-access layer over PostgreSQL + pgvector. It is the
// only place that talks to the database.
//
// It owns a single pgxpool.Pool. Corpus and vector queries use the pool
// directly (see embeddings.go). The pool is the single source of connections:
// when a relational/auth domain arrives and wants gorm, build a *sql.DB from
// THIS pool via stdlib.OpenDBFromPool(s.Pool()) and hand it to gorm — one pool,
// no second connection budget, no rewrite here.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EmbeddingDimensions must match chunks.embedding's vector(N) width
// (migrations/0005_rebuild_corpus.sql). A provider/model swap that changes
// vector size silently corrupts the HNSW index — callers that construct an
// embedder.Service should assert Dimensions() against this before wiring it
// into ingest/retriever (see cmd/api, cmd/crawler, cmd/seed).
const EmbeddingDimensions = 1024

// Store holds the connection pool. Safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a pool from dsn, tunes it, and pings the server so a bad config
// fails at startup rather than on the first query. Call once at startup and
// defer Close.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: parse dsn: %w", err)
	}

	// Pool tuning. NOT one-size-fits-all — revisit under real load and against
	// the server's max_connections.
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 10 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool so a future data-access layer (e.g. a gorm
// handle from stdlib.OpenDBFromPool) can share this one pool.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases all connections. Call once at shutdown.
func (s *Store) Close() { s.pool.Close() }
