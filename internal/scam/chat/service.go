// Package chat is the conversational use case behind POST /v1/chat. It routes
// each multi-turn request into one of three flows — questions about the project
// (IntentProject), about the founder (IntentFounder), or, by default, the
// scam-detection pipeline (IntentScam) — and always fails safe toward scam
// detection so a scam can never be routed away from being checked.
//
// Construction + the Chatter struct and its narrow dependency interfaces live
// here (start here); DTOs in type.go; the orchestration in chat.go; the LLM
// router in route.go; the project/founder answerer in answer.go; the
// deterministic safety heuristic in suspicious.go. Counterpart to
// internal/scam/analyzer.
package chat

import (
	"context"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

const (
	defaultTopK          = 5
	defaultConfidence    = 0.7 // min confidence to trust a project/founder classification
	defaultHistoryWindow = 10  // how many prior messages to feed as context
)

// Classifier decides a turn's intent. *LLMRouter satisfies it; tests supply a
// fake. A returned error is a signal to fail safe (default to scam), not a
// request to abort — Chatter owns that policy.
type Classifier interface {
	Classify(ctx context.Context, history []Message, latest string) (Intent, float64, error)
}

// Retriever is the narrow slice of the retrieval pipeline the scam flow needs
// (mirrors analyze.Retriever). *retriever.Retriever satisfies it.
type Retriever interface {
	HybridSearch(ctx context.Context, query string, k int) ([]retriever.Result, error)
}

// Answerer produces the conversational reply for the project/founder flows.
// *LLMAnswerer satisfies it; tests supply a fake.
type Answerer interface {
	Answer(ctx context.Context, intent Intent, history []Message, latest string) (string, error)
}

// Chatter orchestrates the three flows. Collaborators are injected (no global
// state). Safe for concurrent use if they are.
type Chatter struct {
	classifier    Classifier
	retriever     Retriever
	scorer        analyzer.Scorer
	answerer      Answerer
	topK          int
	confidence    float64
	historyWindow int
}

// Option configures a Chatter. Zero-value defaults are applied in New.
type Option func(*Chatter)

// WithTopK overrides how many scam patterns are retrieved on a scam turn.
// Non-positive values are ignored.
func WithTopK(n int) Option {
	return func(c *Chatter) {
		if n > 0 {
			c.topK = n
		}
	}
}

// WithConfidenceThreshold overrides the minimum router confidence required to
// trust a project/founder classification; below it, the turn falls back to
// scam. Values outside (0,1] are ignored.
func WithConfidenceThreshold(t float64) Option {
	return func(c *Chatter) {
		if t > 0 && t <= 1 {
			c.confidence = t
		}
	}
}

// WithHistoryWindow overrides how many prior messages are fed as context.
// Non-positive values are ignored.
func WithHistoryWindow(n int) Option {
	return func(c *Chatter) {
		if n > 0 {
			c.historyWindow = n
		}
	}
}

// New builds a Chatter. Unset options fall back to defaults (topK=5,
// confidence=0.7, historyWindow=10).
func New(classifier Classifier, ret Retriever, scorer analyzer.Scorer, answerer Answerer, opts ...Option) *Chatter {
	c := &Chatter{
		classifier:    classifier,
		retriever:     ret,
		scorer:        scorer,
		answerer:      answerer,
		topK:          defaultTopK,
		confidence:    defaultConfidence,
		historyWindow: defaultHistoryWindow,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
