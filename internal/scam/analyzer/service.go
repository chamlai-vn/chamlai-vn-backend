// Package analyzer is the scoring use case: suspicious text + retrieved scam
// patterns → a red/yellow/green verdict via an LLM with forced tool use.
// Construction in service.go; the AnalysisResult DTO + tool schema in type.go;
// behaviour in score.go; prompts in prompt.go. Counterpart to
// internal/scam/retriever.
package analyzer

import (
	"context"
	"encoding/json"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

const defaultMaxChunks = 5

// LLM is the narrow slice of llm.Service the analyzer needs. *llm.Anthropic
// satisfies it; tests supply a fake.
type LLM interface {
	GenerateStructured(ctx context.Context, req llm.Request) (json.RawMessage, error)
}

// Scorer is the public contract the API layer depends on. *Analyzer satisfies it.
type Scorer interface {
	Score(ctx context.Context, suspiciousText string, chunks []retriever.Result) (*AnalysisResult, error)
}

// Analyzer scores suspicious text against retrieved scam patterns. Safe for
// concurrent use if its LLM is (the Anthropic client is).
type Analyzer struct {
	llm       LLM
	maxChunks int
}

// Option configures an Analyzer. Zero-value defaults are applied in New.
type Option func(*Analyzer)

// WithMaxChunks caps how many retrieved chunks are sent as context. Non-positive
// values are ignored (mirrors retriever.WithDefaultTopK).
func WithMaxChunks(n int) Option {
	return func(a *Analyzer) {
		if n > 0 {
			a.maxChunks = n
		}
	}
}

// New builds an Analyzer over client. Unset options fall back to defaults
// (maxChunks=5).
func New(client LLM, opts ...Option) *Analyzer {
	a := &Analyzer{
		llm:       client,
		maxChunks: defaultMaxChunks,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

var _ Scorer = (*Analyzer)(nil)
