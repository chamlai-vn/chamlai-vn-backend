package model

import "time"

// ChunkKind distinguishes a chunk embedded from a document's Content section
// from one embedded from one of its synthetic User query lines (doc2query).
// Mirrors the chunks.kind CHECK constraint in migrations/0005_rebuild_corpus.sql
// — keep the two in sync.
type ChunkKind string

const (
	ChunkKindContent ChunkKind = "content"
	ChunkKindQuery   ChunkKind = "query"
)

// Chunk is one embedded piece of a Document: either a slice of its Content
// (ChunkKindContent) or one of its synthetic user-query lines
// (ChunkKindQuery) — the multi-representation retrieval unit.
type Chunk struct {
	ID         int64
	DocumentID int64
	Kind       ChunkKind
	Content    string
	Embedding  []float32
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// ContentTSV []float32: not exposed to application layer
}
