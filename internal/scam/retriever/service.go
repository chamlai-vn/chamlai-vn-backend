package retriever

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/reranker"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

const (
	defaultTopKVal = 5

	// candidateTopK is how many candidate CHUNKS both Retrieve and
	// HybridSearch pull from each arm (vector, keyword) before per-document
	// dedupe. Raised from the original 20 to 80: with multi-representation
	// embedding, a single document contributes several near-duplicate
	// vectors (its content chunk(s) plus every doc2query "# User query"
	// line — typically 3-6+), which cluster tightly in the ANN ranking. At
	// the old candidateTopK=20, one strong document could occupy most of
	// the window, starving the post-dedupe result of other distinct
	// documents entirely. Rule of thumb from the plan's dedupe-math review:
	// candidateTopK should be >= rerankCandidates × the corpus's typical
	// vectors-per-document; 80 is the pragmatic number for the current
	// corpus size, not a hard derivation — revisit together with
	// hnswEfSearch (internal/infra/store.hnswEfSearch, which must stay >=
	// this) as the corpus grows. Must stay >= any topK callers pass in.
	candidateTopK = 80

	// rrfK is the Reciprocal Rank Fusion damping constant: score += 1/(rrfK+rank+1).
	// 60 is the standard value from the original RRF paper and remains
	// current best practice (Elasticsearch/Azure AI Search/Milvus all ship
	// it as their default) — unchanged by the multi-representation rework.
	rrfK = 60

	// rerankCandidates is how many fused (already per-document-deduped)
	// results feed the reranker when one is configured. Tune together with
	// candidateTopK when the corpus grows — benchmark-driven, not by feel.
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
