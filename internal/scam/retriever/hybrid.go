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
// (keyword wins). Both arms fetch candidateTopK candidates; each arm is then
// collapsed to one entry per document — keeping only its best-ranked chunk,
// see collapseToDocuments — BEFORE fusion, so a document with many multi-
// representation vectors can't dominate RRF simply by appearing more often
// within a single arm.
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

	// Per-arm collapse BEFORE fusion: a document that shows up at multiple
	// ranks within a single arm (its content chunk at rank 2, one of its
	// query chunks at rank 5, say) must contribute only its best rank —
	// otherwise reciprocalRankFusion would sum multiple contributions from
	// one arm alone, reintroducing exactly the count-domination multi-
	// representation embedding is designed to avoid.
	vectorHits = collapseToDocuments(vectorHits)
	keywordHits = collapseToDocuments(keywordHits)

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

// reciprocalRankFusion merges two already per-document-deduped, rank-ordered
// arms by summing 1/(rrfK+rank+1) per arm for each document (rank is
// 0-based), then returns the topK documents by fused score, highest first. A
// document present in both arms accumulates both contributions, so results
// both arms agree on rank highest. Ties are broken by DocumentID ascending
// for deterministic output.
//
// vectorHits and keywordHits MUST already be collapsed to one entry per
// document (see collapseToDocuments) before calling this — this function
// does not dedupe within an arm itself. If a single arm contained the same
// document at multiple ranks, that arm alone would accumulate multiple RRF
// contributions for it, reintroducing the count-domination multi-
// representation embedding is designed to avoid.
func reciprocalRankFusion(vectorHits, keywordHits []store.Match, topK int) []Result {
	scores := make(map[int64]float64)
	payload := make(map[int64]store.Match)

	add := func(hits []store.Match) {
		for rank, m := range hits {
			scores[m.DocumentID] += 1.0 / float64(rrfK+rank+1)
			if _, ok := payload[m.DocumentID]; !ok {
				payload[m.DocumentID] = m
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
