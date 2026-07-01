package retriever

import (
	"context"
	"fmt"
	"sort"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// HybridSearch fuses the vector (semantic) and keyword (lexical, ts_rank) arms
// with Reciprocal Rank Fusion, so a query is served well whether it's dominated
// by meaning (vector wins) or by a distinctive term an embedding can dilute
// (keyword wins). Both arms fetch candidateTopK candidates before fusion.
//
// Fails fast: an error embedding the query, or from either arm, aborts the
// whole search rather than silently degrading to one arm. Both arms share the
// same DB/embedder, so a failure here is systemic, not partial — and a scam
// retriever that quietly drops half its signal is worse than a clear error.
//
// Result.Score on the returned values is the fused RRF score, not a cosine
// similarity — callers that only read Content/ScamType (as analyzer.Score does)
// are unaffected; only the order and membership of results matter downstream.
func (r *Retriever) HybridSearch(ctx context.Context, query string, topK int) ([]Result, error) {
	q, err := validateQuery(query)
	if err != nil {
		return nil, err
	}

	if topK <= 0 {
		topK = r.defaultTopK
	}

	vecs, err := r.emb.Embed(ctx, []string{q}, embedder.InputQuery)
	if err != nil {
		return nil, fmt.Errorf("retriever: hybrid: embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("retriever: hybrid: embed returned %d vectors for 1 query", len(vecs))
	}

	vectorHits, err := r.store.SearchSimilar(ctx, vecs[0], candidateTopK)
	if err != nil {
		return nil, fmt.Errorf("retriever: hybrid: vector search: %w", err)
	}

	keywordHits, err := r.store.SearchByKeyword(ctx, q, candidateTopK)
	if err != nil {
		return nil, fmt.Errorf("retriever: hybrid: keyword search: %w", err)
	}

	return reciprocalRankFusion(vectorHits, keywordHits, topK), nil
}

// reciprocalRankFusion merges two rank-ordered arms by summing 1/(rrfK+rank+1)
// per arm for each chunk (rank is 0-based), then returns the topK chunks by
// fused score, highest first. A chunk present in both arms accumulates both
// contributions, so results that both arms agree on rank highest. Ties are
// broken by ChunkID ascending for deterministic output.
func reciprocalRankFusion(vectorHits, keywordHits []store.Match, topK int) []Result {
	scores := make(map[int64]float64)
	payload := make(map[int64]store.Match)

	add := func(hits []store.Match) {
		for rank, m := range hits {
			scores[m.ChunkID] += 1.0 / float64(rrfK+rank+1)
			if _, ok := payload[m.ChunkID]; !ok {
				payload[m.ChunkID] = m
			}
		}
	}
	add(vectorHits)
	add(keywordHits)

	ids := make([]int64, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > topK {
		ids = ids[:topK]
	}

	out := make([]Result, len(ids))
	for i, id := range ids {
		res := matchToResult(payload[id])
		res.Score = scores[id]
		out[i] = res
	}
	return out
}
