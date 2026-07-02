package retriever

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/reranker"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

const (
	defaultTopKVal = 5

	// candidateTopK is how many candidates HybridSearch pulls from each arm
	// (vector, keyword) before fusion. Must be >= any topK callers pass in, or
	// RRF has fewer candidates than the caller asked for.
	candidateTopK = 20

	// rrfK is the Reciprocal Rank Fusion damping constant: score += 1/(rrfK+rank+1).
	// 60 is the standard value from the original RRF paper.
	rrfK = 60

	// rerankCandidates is how many fused results feed the reranker when one is
	// configured (~4x the default final topK: recall headroom for the
	// cross-encoder without paying latency for a longer tail). Tune together
	// with candidateTopK when the corpus grows — benchmark-driven, not by feel.
	rerankCandidates = 20
)

// Store is the persistence the retriever needs. *store.Store satisfies it;
// tests supply a fake. SearchSimilar is the vector (semantic) arm, SearchByKeyword
// the lexical arm used by HybridSearch.
type Store interface {
	SearchSimilar(ctx context.Context, query []float32, k int) ([]store.Match, error)
	SearchByKeyword(ctx context.Context, query string, k int) ([]store.Match, error)
}

// Retriever runs the query-side of the RAG pipeline. Safe for concurrent use
// if its embedder, store, and reranker are (all are).
type Retriever struct {
	emb         embedder.Service
	store       Store
	reranker    reranker.Service
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

// WithReranker enables a rerank stage after rank fusion in HybridSearch: the
// top rerankCandidates fused results are scored by rr and truncated to topK.
// A nil reranker is ignored — rerank stays off, which is the default, and
// HybridSearch behaves exactly as it does without this option.
func WithReranker(rr reranker.Service) Option {
	return func(r *Retriever) {
		if rr != nil {
			r.reranker = rr
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
