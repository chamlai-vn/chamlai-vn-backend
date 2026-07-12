// Package analyze is the HTTP layer over the scam-scoring pipeline: it
// decodes a request, retrieves similar scam patterns, scores the message
// with the LLM, and returns the verdict as JSON. Construction + the Handler
// struct live in service.go (start here); the request DTO in type.go; the
// endpoint in analyze.go.
package analyze

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

// DefaultTopK is how many scam patterns are retrieved per request when
// WithTopK isn't set. Exported so callers that need to mirror this handler's
// retrieval fidelity outside HTTP (e.g. cmd/benchmark) reference the real
// production value instead of re-declaring their own copy of "5".
const DefaultTopK = 5

// Retriever is the narrow slice of the retrieval pipeline the handler needs.
// *retriever.Retriever satisfies it; tests supply a fake. Kept narrow — the
// handler only ever asks for the top-k similar patterns, via the hybrid
// (vector + keyword, RRF-fused, optionally reranked) search path.
type Retriever interface {
	HybridSearch(ctx context.Context, query string, k int) ([]retriever.Result, error)
}

// Budget gates access to the paid pipeline (Voyage embed + Claude scoring)
// behind a global daily cap — the wallet safety net, independent of and in
// addition to any per-IP rate limiting. Reserve claims one slot of today's
// budget; ok=false means the daily cap has been reached and the caller must
// not proceed to retrieval/scoring. A non-nil error means the budget could
// not be verified at all, and the caller must fail closed (reject the
// request) rather than risk an unbudgeted paid call.
type Budget interface {
	Reserve(ctx context.Context) (ok bool, err error)
}

// Handler serves POST /v1/analyze over the retrieve→score pipeline. All
// collaborators are injected (no global state). Safe for concurrent use if
// they are (they are).
type Handler struct {
	retriever Retriever
	scorer    analyzer.Scorer
	budget    Budget
	topK      int
}

// Option configures a Handler. Zero-value defaults are applied in New.
type Option func(*Handler)

// WithTopK overrides how many scam patterns are retrieved per request.
// Non-positive values are ignored (mirrors retriever.WithDefaultTopK).
func WithTopK(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.topK = n
		}
	}
}

// New builds a Handler over ret, scorer, and budget. Unset options fall back
// to defaults (topK=5).
func New(ret Retriever, scorer analyzer.Scorer, budget Budget, opts ...Option) *Handler {
	h := &Handler{
		retriever: ret,
		scorer:    scorer,
		budget:    budget,
		topK:      DefaultTopK,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
