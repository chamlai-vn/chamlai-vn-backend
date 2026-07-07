// Package retriever is the query-side use case: suspicious text → top-k
// similar scam-pattern documents, scored. Counterpart to internal/scam/ingest.
// Construction in service.go; the Result DTO in type.go; behaviour here.
package retriever

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// Retrieve embeds query with query intent, fetches candidateTopK nearest
// corpus chunks, collapses them to distinct documents (keeping each
// document's best-ranked/lowest-distance chunk — see collapseToDocuments),
// and returns the topK documents sorted by similarity score (highest first).
//
// candidateTopK, not topK, is what's fetched from the store: with multi-
// representation embedding a document contributes several near-duplicate
// vectors (its content plus each doc2query "# User query" line), so
// fetching only topK chunks risks a single strong document occupying most
// of the window and starving the collapse of other distinct documents.
//
// topK <= 0 uses the configured default (5). The query is validated as 1–5000
// runes (trimmed); runes, not bytes, because Vietnamese is multibyte.
func (r *Retriever) Retrieve(ctx context.Context, query string, topK int) ([]Result, error) {
	q, err := validateQuery(query)
	if err != nil {
		return nil, err
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

	matches, err := r.store.SearchSimilar(ctx, vecs[0], candidateTopK)
	if err != nil {
		return nil, fmt.Errorf("retriever: search: %w", err)
	}

	deduped := collapseToDocuments(matches)
	if len(deduped) > topK {
		deduped = deduped[:topK]
	}

	out := make([]Result, len(deduped))
	for i, m := range deduped {
		out[i] = matchToResult(m)
	}
	return out, nil
}

// collapseToDocuments drops every match after a document's first occurrence,
// preserving input order. matches is expected to already be rank-ordered by
// its source query (closest-first for SearchSimilar, best-ts_rank-first for
// SearchByKeyword), so "first occurrence" is exactly "best rank" for that
// arm — the max-style, not sum-style, aggregation multi-representation
// embedding requires: a document with many query-chunk vectors must not get
// extra weight just for appearing more often in the candidate list.
func collapseToDocuments(matches []store.Match) []store.Match {
	seen := make(map[int64]bool, len(matches))
	out := make([]store.Match, 0, len(matches))
	for _, m := range matches {
		if seen[m.DocumentID] {
			continue
		}
		seen[m.DocumentID] = true
		out = append(out, m)
	}
	return out
}

// validateQuery trims query and checks it is 1–5000 runes (not bytes — Vietnamese
// is multibyte). Shared by Retrieve and HybridSearch.
func validateQuery(query string) (string, error) {
	q := strings.TrimSpace(query)
	n := utf8.RuneCountInString(q)
	if n == 0 {
		return "", fmt.Errorf("retriever: empty query")
	}
	if n > 5000 {
		return "", fmt.Errorf("retriever: query too long (%d chars, max 5000)", n)
	}
	return q, nil
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

// matchToResult maps a store.Match to a doc-centric Result. Content always
// comes from m.DocumentContent (the document's full body), never
// m.Content (the specific chunk that matched) — a query-vector match must
// ground the analyzer in the actual scam-warning text, not the victim-style
// question that happened to retrieve it. Used by the vector arm of
// HybridSearch (fusion later overwrites Score with the fused RRF score).
func matchToResult(m store.Match) Result {
	return Result{
		DocumentID: m.DocumentID,
		Title:      m.DocumentTitle,
		Content:    m.DocumentContent,
		Prevention: m.DocumentPrevention,
		ScamType:   m.ScamType,
		SourceURL:  m.SourceURL,
		Score:      scoreFromDistance(m.Distance),
	}
}
