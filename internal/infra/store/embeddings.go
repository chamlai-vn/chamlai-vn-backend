package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
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

// InsertDocumentWithChunks stores a document and all its chunks in a single
// transaction, returning the new document id. Either everything lands or
// nothing does — so a failure partway through the chunks can't leave an
// orphan document or a half-indexed one behind. Returns an error if chunks is
// empty (a document with no chunks is never useful for retrieval).
//
// chunks are model.Chunk so ingest builds its multi-representation set
// (content chunks and doc2query user-query chunks, see model.ChunkKind)
// directly against the shared model type — store has no chunk type of its own.
func (s *Store) InsertDocumentWithChunks(ctx context.Context, doc model.Document, chunks []model.Chunk) (int64, error) {
	if len(chunks) == 0 {
		return 0, fmt.Errorf("store: no chunks to insert for %q", doc.URL)
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
		`INSERT INTO documents (url, title, content, prevention, scam_type, source)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		doc.URL, doc.Title, doc.Content, doc.Prevention, doc.ScamType, doc.Source).Scan(&docID); err != nil {
		return 0, fmt.Errorf("store: insert document: %w", err)
	}

	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(
			`INSERT INTO chunks (document_id, kind, content, embedding)
			 VALUES ($1,$2,$3,$4::vector)`,
			docID, string(c.Kind), c.Content, vecLiteral(c.Embedding))
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

// Match is one retrieved chunk and how close it is to the query vector, plus
// the parent document's fields needed downstream (grounding text, attribution,
// reference advice). Content is the matched chunk's own text (what was
// embedded/indexed — a content slice or a doc2query user-query line);
// DocumentContent is the full parent document body. Distinct from model.Chunk:
// this is the read-model returned by a query, never written back.
type Match struct {
	ChunkID            int64
	DocumentID         int64
	Kind               model.ChunkKind
	Content            string
	DocumentTitle      string
	DocumentContent    string
	DocumentPrevention string
	ScamType           string
	SourceURL          string  // documents.url — source article for attribution
	Distance           float64 // cosine distance — smaller is more similar
}

// SearchSimilar returns the top-k chunks nearest to query, closest first.
func (s *Store) SearchSimilar(ctx context.Context, query []float32, k int) ([]Match, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, c.kind, c.content,
		        d.title, d.content, d.prevention, d.scam_type, d.url,
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
		var kind string
		if err := rows.Scan(&m.ChunkID, &m.DocumentID, &kind, &m.Content,
			&m.DocumentTitle, &m.DocumentContent, &m.DocumentPrevention, &m.ScamType, &m.SourceURL,
			&m.Distance); err != nil {
			return nil, err
		}
		m.Kind = model.ChunkKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchByKeyword returns the top-k chunks whose content_tsv matches query,
// ranked by ts_rank (a TF-IDF-style score — not true BM25), best first. This is
// the lexical counterpart to SearchSimilar: it recovers chunks containing rare
// or distinctive terms that a vector search can dilute into a generic direction.
// Match.Distance is left at its zero value — there is no cosine distance on this
// path; retriever.HybridSearch fuses by rank position, not by Distance.
//
// query and content_tsv both use the 'vietnamese' text-search config (see
// migrations/0005_rebuild_corpus.sql) with unaccent folding, so accent-
// insensitive input ("lua dao") still matches accented content ("lừa đảo").
func (s *Store) SearchByKeyword(ctx context.Context, query string, k int) ([]Match, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, c.kind, c.content,
		        d.title, d.content, d.prevention, d.scam_type, d.url
		 FROM chunks c JOIN documents d ON d.id = c.document_id
		 WHERE c.content_tsv @@ plainto_tsquery('vietnamese', unaccent($1))
		 ORDER BY ts_rank(c.content_tsv, plainto_tsquery('vietnamese', unaccent($1))) DESC
		 LIMIT $2`,
		query, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		var kind string
		if err := rows.Scan(&m.ChunkID, &m.DocumentID, &kind, &m.Content,
			&m.DocumentTitle, &m.DocumentContent, &m.DocumentPrevention, &m.ScamType, &m.SourceURL); err != nil {
			return nil, err
		}
		m.Kind = model.ChunkKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListChunks returns up to limit chunks ordered by id, for offline tooling
// (e.g. benchmark dataset generation) that needs raw corpus content rather
// than a query match. Not on the retrieval hot path — Distance is left at its
// zero value like SearchByKeyword.
func (s *Store) ListChunks(ctx context.Context, limit int) ([]Match, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, c.kind, c.content,
		        d.title, d.content, d.prevention, d.scam_type, d.url
		 FROM chunks c JOIN documents d ON d.id = c.document_id
		 ORDER BY c.id LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var m Match
		var kind string
		if err := rows.Scan(&m.ChunkID, &m.DocumentID, &kind, &m.Content,
			&m.DocumentTitle, &m.DocumentContent, &m.DocumentPrevention, &m.ScamType, &m.SourceURL); err != nil {
			return nil, err
		}
		m.Kind = model.ChunkKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}
