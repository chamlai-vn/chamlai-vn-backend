package store

import (
	"context"
	"strconv"
	"strings"
)

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

// Match is one retrieved chunk and how close it is to the query vector.
type Match struct {
	ChunkID  int64
	Content  string
	ScamType string
	Distance float64 // cosine distance — smaller is more similar
}

// SearchSimilar returns the top-k chunks nearest to query, closest first.
func (s *Store) SearchSimilar(ctx context.Context, query []float32, k int) ([]Match, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.content, d.scam_type, c.embedding <=> $1::vector AS distance
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
		if err := rows.Scan(&m.ChunkID, &m.Content, &m.ScamType, &m.Distance); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
