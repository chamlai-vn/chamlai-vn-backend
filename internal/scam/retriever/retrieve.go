// Package retriever is the query-side use case: suspicious text → top-k similar
// scam-pattern chunks, scored. Counterpart to internal/scam/ingest. Construction
// in service.go; the Result DTO in type.go; behaviour here.
package retriever

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
)

// Retrieve embeds query with query intent, fetches the topK nearest corpus
// chunks, and returns them sorted by similarity score (highest first, matching
// the ORDER BY distance ASC from the store).
//
// topK <= 0 uses the configured default (5). The query is validated as 1–5000
// runes (trimmed); runes, not bytes, because Vietnamese is multibyte.
func (r *Retriever) Retrieve(ctx context.Context, query string, topK int) ([]Result, error) {
	q := strings.TrimSpace(query)
	n := utf8.RuneCountInString(q)
	if n == 0 {
		return nil, fmt.Errorf("retriever: empty query")
	}
	if n > 5000 {
		return nil, fmt.Errorf("retriever: query too long (%d chars, max 5000)", n)
	}

	if topK <= 0 {
		topK = r.defaultTopK
	}

	vecs, err := r.emb.Embed(ctx, []string{q}, embedder.InputQuery)
	if err != nil {
		return nil, fmt.Errorf("retriever: embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("retriever: embed returned %d vectors for 1 query", len(vecs))
	}

	matches, err := r.store.SearchSimilar(ctx, vecs[0], topK)
	if err != nil {
		return nil, fmt.Errorf("retriever: search: %w", err)
	}

	out := make([]Result, len(matches))
	for i, m := range matches {
		out[i] = Result{
			ChunkID:    m.ChunkID,
			DocumentID: m.DocumentID,
			Content:    m.Content,
			ScamType:   m.ScamType,
			SourceURL:  m.SourceURL,
			Score:      scoreFromDistance(m.Distance),
		}
	}
	return out, nil
}

// scoreFromDistance converts cosine distance (∈ [0,2]) to a [0,1] similarity.
// A future min-score threshold would filter results here; for now all k results
// are returned — scoring is a mechanism, relevance judgement belongs in the analyzer.
func scoreFromDistance(d float64) float64 {
	s := 1 - d
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}
