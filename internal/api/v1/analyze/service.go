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

const defaultTopK = 5

// Retriever is the narrow slice of the retrieval pipeline the handler needs.
// *retriever.Retriever satisfies it; tests supply a fake. Kept narrow — the
// handler only ever asks for the top-k similar patterns.
type Retriever interface {
	Retrieve(ctx context.Context, query string, k int) ([]retriever.Result, error)
}

// Handler serves POST /v1/analyze over the retrieve→score pipeline. Both
// collaborators are injected (no global state). Safe for concurrent use if
// they are (they are).
type Handler struct {
	retriever Retriever
	scorer    analyzer.Scorer
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

// New builds a Handler over ret and scorer. Unset options fall back to
// defaults (topK=5).
func New(ret Retriever, scorer analyzer.Scorer, opts ...Option) *Handler {
	h := &Handler{
		retriever: ret,
		scorer:    scorer,
		topK:      defaultTopK,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
