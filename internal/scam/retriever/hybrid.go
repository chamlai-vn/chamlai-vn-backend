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
// When a reranker is configured (WithReranker), the top rerankCandidates fused
// results are passed through it and truncated to topK by relevance; otherwise
// fusion itself produces the final topK. Either way Result.Score is not a
// cosine similarity: it's the fused RRF score with no reranker, or the
// reranker's relevance score with one — callers that only read
// Content/ScamType (as analyzer.Score does) are unaffected; only the order and
// membership of results matter downstream.
//
// Fails fast: an error embedding the query, from either arm, or from the
// reranker, aborts the whole search rather than silently degrading. All three
// share the same DB/embedder/reranker, so a failure here is systemic, not
// partial — and a scam retriever that quietly drops signal is worse than a
// clear error.
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

	fusionK := topK
	if r.reranker != nil {
		fusionK = rerankCandidates
	}
	fused := reciprocalRankFusion(vectorHits, keywordHits, fusionK)

	if r.reranker == nil || len(fused) == 0 {
		return fused, nil
	}
	return r.rerank(ctx, q, fused, topK)
}

// rerank scores fused candidates with r.reranker and returns the top topK by
// relevance. Results reference fused by index, so this maps them back to the
// original Result (content, scam type, etc.), overwriting Score with the
// reranker's relevance score.
func (r *Retriever) rerank(ctx context.Context, query string, fused []Result, topK int) ([]Result, error) {
	docs := make([]string, len(fused))
	for i, f := range fused {
		docs[i] = f.Content
	}

	ranked, err := r.reranker.Rerank(ctx, query, docs, topK)
	if err != nil {
		return nil, fmt.Errorf("retriever: hybrid: rerank: %w", err)
	}

	out := make([]Result, len(ranked))
	for i, rr := range ranked {
		res := fused[rr.Index]
		res.Score = rr.RelevanceScore
		out[i] = res
	}
	return out, nil
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
