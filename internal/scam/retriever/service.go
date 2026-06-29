package retriever

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

const defaultTopKVal = 5

// Store is the persistence the retriever needs. *store.Store satisfies it;
// tests supply a fake. Kept narrow — the retriever only ever does ANN search.
type Store interface {
	SearchSimilar(ctx context.Context, query []float32, k int) ([]store.Match, error)
}

// Retriever runs the query-side of the RAG pipeline. Safe for concurrent use
// if its embedder and store are (both are).
type Retriever struct {
	emb         embedder.Service
	store       Store
	defaultTopK int
}

// Option configures a Retriever. Zero-value defaults are applied in New.
type Option func(*Retriever)

// WithDefaultTopK overrides the k used when Retrieve is called with topK <= 0.
// Non-positive values are ignored (mirrors ingest.WithBatchSize).
func WithDefaultTopK(n int) Option {
	return func(r *Retriever) {
		if n > 0 {
			r.defaultTopK = n
		}
	}
}

// New builds a Retriever over emb and st. Unset options fall back to defaults
// (defaultTopK=5).
func New(emb embedder.Service, st Store, opts ...Option) *Retriever {
	r := &Retriever{
		emb:         emb,
		store:       st,
		defaultTopK: defaultTopKVal,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}
