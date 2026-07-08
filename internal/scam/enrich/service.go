package enrich

import (
	"context"
	"encoding/json"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
)

// LLM is the narrow slice of llm.Service enrich needs. Constructed via
// llm.New in cmd/crawler and handed in here — Enricher never constructs a
// provider directly (mirrors analyzer.LLM).
type LLM interface {
	GenerateStructured(ctx context.Context, req llm.Request) (json.RawMessage, error)
}

// Enricher turns raw crawled text into a corpusdoc.Document via a forced
// tool call. Safe for concurrent use if its LLM is.
type Enricher struct {
	llm LLM
}

// New builds an Enricher over client.
func New(client LLM) *Enricher {
	return &Enricher{llm: client}
}
