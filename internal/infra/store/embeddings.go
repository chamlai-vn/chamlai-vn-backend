package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is Postgres' SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). The crawler uses it to treat a racing duplicate
// url — two workers that both passed DocumentExists before either inserted — as
// a skip rather than a hard error.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// DocumentExists reports whether a document with this url is already stored.
// The crawler calls this before embedding so a re-run skips known urls without
// paying for embedding-provider calls. It is only an optimisation, not a
// guarantee: the UNIQUE(url) constraint is what actually prevents duplicates
// (see IsUniqueViolation).
func (s *Store) DocumentExists(ctx context.Context, url string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM documents WHERE url=$1)`, url).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: document exists %q: %w", url, err)
	}
	return exists, nil
}

// vecLiteral renders a float32 vector as pgvector's text form "[0.1,0.2,...]".
// Paired with a $n::vector cast, this lets us store/query vectors without
// pulling in a pgvector-specific driver dependency.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// InsertDocument stores one scam-warning article and returns its id.
func (s *Store) InsertDocument(ctx context.Context, url, title, content, scamType, source string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO documents (url, title, content, scam_type, source)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		url, title, content, scamType, source).Scan(&id)
	return id, err
}

// InsertChunk stores one chunk of a document together with its embedding.
func (s *Store) InsertChunk(ctx context.Context, docID int64, content string, emb []float32) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chunks (document_id, content, embedding)
		 VALUES ($1,$2,$3::vector)`,
		docID, content, vecLiteral(emb))
	return err
}

// Chunk is one piece of document text paired with its embedding, ready to store.
type Chunk struct {
	Content   string
	Embedding []float32
}

// InsertDocumentWithChunks stores a document and all its chunks in a single
// transaction, returning the new document id. Either everything lands or
// nothing does — so a failure partway through the chunks can't leave an
// orphan document or a half-indexed one behind. Returns an error if chunks is
// empty (a document with no chunks is never useful for retrieval).
func (s *Store) InsertDocumentWithChunks(ctx context.Context, url, title, content, scamType, source string, chunks []Chunk) (int64, error) {
	if len(chunks) == 0 {
		return 0, fmt.Errorf("store: no chunks to insert for %q", url)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	// Rollback is a no-op once the tx is committed, so this is safe to always
	// defer. The error is intentionally ignored: on the commit path it returns
	// ErrTxClosed, and on a failure path we already return the original error.
	defer func() { _ = tx.Rollback(ctx) }()

	var docID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO documents (url, title, content, scam_type, source)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		url, title, content, scamType, source).Scan(&docID); err != nil {
		return 0, fmt.Errorf("store: insert document: %w", err)
	}

	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(
			`INSERT INTO chunks (document_id, content, embedding)
			 VALUES ($1,$2,$3::vector)`,
			docID, c.Content, vecLiteral(c.Embedding))
	}
	br := tx.SendBatch(ctx, batch)
	for i := range chunks {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, fmt.Errorf("store: insert chunk %d: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return 0, fmt.Errorf("store: close batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return docID, nil
}

// Match is one retrieved chunk and how close it is to the query vector.
type Match struct {
	ChunkID    int64
	DocumentID int64
	Content    string
	ScamType   string
	SourceURL  string  // documents.url — source article for attribution
	Distance   float64 // cosine distance — smaller is more similar
}

// SearchSimilar returns the top-k chunks nearest to query, closest first.
func (s *Store) SearchSimilar(ctx context.Context, query []float32, k int) ([]Match, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, c.content, d.scam_type, d.url,
		        c.embedding <=> $1::vector AS distance
		 FROM chunks c JOIN documents d ON d.id = c.document_id
		 ORDER BY distance LIMIT $2`,
		vecLiteral(query), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.ChunkID, &m.DocumentID, &m.Content, &m.ScamType, &m.SourceURL, &m.Distance); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
