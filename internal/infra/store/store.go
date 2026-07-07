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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmbeddingDimensions must match chunks.embedding's vector(N) width
// (migrations/0005_rebuild_corpus.sql). A provider/model swap that changes
// vector size silently corrupts the HNSW index — callers that construct an
// embedder.Service should assert Dimensions() against this before wiring it
// into ingest/retriever (see cmd/api, cmd/crawler, cmd/seed).
const EmbeddingDimensions = 1024

// hnswEfSearch is the HNSW query-time recall knob (session GUC, Postgres
// default 40). Must stay >= retriever.candidateTopK, or the ANN search
// starves before the retriever's overfetch has a chance to work: with
// multi-representation embedding, a document's several near-duplicate
// vectors cluster tightly in the graph, so a low ef_search can fill the
// candidate window with one document's own vectors before the search frontier
// reaches other, distinct documents. Set once per physical connection (not
// per query) via AfterConnect below — cheaper than a SET LOCAL/transaction
// wrapper around every SearchSimilar call, and correct here because nothing
// else in this codebase needs a different value.
const hnswEfSearch = 100

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

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET hnsw.ef_search = %d", hnswEfSearch))
		return err
	}

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
