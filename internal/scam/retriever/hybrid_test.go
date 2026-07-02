package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/reranker"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// fakeReranker records the query/documents/topK it was called with and
// returns canned results, so tests can assert on both the request it received
// (does it see the fused candidates in order?) and how its response is mapped
// back to Result.
type fakeReranker struct {
	called    bool
	lastQuery string
	lastDocs  []string
	lastTopK  int

	results []reranker.Result
	err     error
}

func (f *fakeReranker) Rerank(_ context.Context, query string, documents []string, topK int) ([]reranker.Result, error) {
	f.called = true
	f.lastQuery = query
	f.lastDocs = documents
	f.lastTopK = topK
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func (f *fakeReranker) Model() string { return "fake-rerank" }

func TestReciprocalRankFusion_AgreementWins(t *testing.T) {
	// Chunk 1 ranks in both arms, chunk 2 only in vector, chunk 3 only in keyword.
	// The chunk both arms agree on should come out on top.
	vector := []store.Match{
		{ChunkID: 2, Content: "vector only"},
		{ChunkID: 1, Content: "in both"},
	}
	keyword := []store.Match{
		{ChunkID: 1, Content: "in both"},
		{ChunkID: 3, Content: "keyword only"},
	}

	out := reciprocalRankFusion(vector, keyword, 3)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3", len(out))
	}
	if out[0].ChunkID != 1 {
		t.Errorf("out[0].ChunkID = %d, want 1 (present in both arms)", out[0].ChunkID)
	}
}

func TestReciprocalRankFusion_Dedup(t *testing.T) {
	// Same chunk in both arms must appear once in the output, not twice.
	vector := []store.Match{{ChunkID: 1, Content: "x"}}
	keyword := []store.Match{{ChunkID: 1, Content: "x"}}

	out := reciprocalRankFusion(vector, keyword, 10)
	if len(out) != 1 {
		t.Fatalf("got %d results, want 1 (deduped)", len(out))
	}
}

func TestReciprocalRankFusion_TopKTruncates(t *testing.T) {
	vector := []store.Match{
		{ChunkID: 1}, {ChunkID: 2}, {ChunkID: 3}, {ChunkID: 4},
	}
	out := reciprocalRankFusion(vector, nil, 2)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (topK truncation)", len(out))
	}
	if out[0].ChunkID != 1 || out[1].ChunkID != 2 {
		t.Errorf("got ChunkIDs %d,%d, want 1,2 (vector rank order preserved)", out[0].ChunkID, out[1].ChunkID)
	}
}

func TestReciprocalRankFusion_EmptyKeywordArmPreservesVectorOrder(t *testing.T) {
	vector := []store.Match{{ChunkID: 5}, {ChunkID: 1}, {ChunkID: 9}}
	out := reciprocalRankFusion(vector, nil, 3)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3", len(out))
	}
	want := []int64{5, 1, 9}
	for i, id := range want {
		if out[i].ChunkID != id {
			t.Errorf("out[%d].ChunkID = %d, want %d", i, out[i].ChunkID, id)
		}
	}
}

func TestReciprocalRankFusion_DeterministicTieBreak(t *testing.T) {
	// Two chunks tied at rank 0 of separate, non-overlapping arms get equal
	// fused scores; output order must be deterministic (ChunkID ascending).
	vector := []store.Match{{ChunkID: 20}}
	keyword := []store.Match{{ChunkID: 10}}

	out := reciprocalRankFusion(vector, keyword, 2)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2", len(out))
	}
	if out[0].ChunkID != 10 || out[1].ChunkID != 20 {
		t.Errorf("got ChunkIDs %d,%d, want 10,20 (tie broken by ChunkID asc)", out[0].ChunkID, out[1].ChunkID)
	}
}

func TestHybridSearch_EmbedsAsQueryAndFetchesCandidateTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	r := New(emb, st)

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 5); err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if emb.lastInput != embedder.InputQuery {
		t.Errorf("input type = %q, want %q", emb.lastInput, embedder.InputQuery)
	}
	if st.lastK != candidateTopK {
		t.Errorf("vector arm k = %d, want candidateTopK=%d", st.lastK, candidateTopK)
	}
	if st.lastKeywordK != candidateTopK {
		t.Errorf("keyword arm k = %d, want candidateTopK=%d", st.lastKeywordK, candidateTopK)
	}
	if st.lastKeywordQ != "lừa đảo" {
		t.Errorf("keyword arm query = %q, want %q", st.lastKeywordQ, "lừa đảo")
	}
}

func TestHybridSearch_FusesAndTruncatesToTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{ChunkID: 1, Content: "vector top"},
			{ChunkID: 2, Content: "vector second"},
		},
		keywordMatches: []store.Match{
			{ChunkID: 1, Content: "vector top"},
			{ChunkID: 3, Content: "keyword only"},
		},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "nghi ngờ", 2)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (topK)", len(results))
	}
	if results[0].ChunkID != 1 {
		t.Errorf("results[0].ChunkID = %d, want 1 (agreed by both arms)", results[0].ChunkID)
	}
}

func TestHybridSearch_DefaultTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{{ChunkID: 1}, {ChunkID: 2}, {ChunkID: 3}, {ChunkID: 4}, {ChunkID: 5}, {ChunkID: 6}},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("got %d results, want 5 (default topK)", len(results))
	}
}

func TestHybridSearch_RejectsEmptyQuery(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	r := New(emb, st)

	if _, err := r.HybridSearch(context.Background(), "   ", 5); err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if st.lastK != 0 || st.lastKeywordK != 0 {
		t.Errorf("store was called after validation failure: lastK=%d lastKeywordK=%d", st.lastK, st.lastKeywordK)
	}
}

func TestHybridSearch_EmbedFailureSkipsBothArms(t *testing.T) {
	emb := &fakeEmbedder{dims: 4, err: errors.New("voyage down")}
	st := &fakeStore{}
	r := New(emb, st)

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 5); err == nil {
		t.Fatal("expected embed error, got nil")
	}
	if st.lastK != 0 || st.lastKeywordK != 0 {
		t.Errorf("store was called after embed failure: lastK=%d lastKeywordK=%d", st.lastK, st.lastKeywordK)
	}
}

func TestHybridSearch_VectorArmErrorFailsFast(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{err: errors.New("db down")}
	r := New(emb, st)

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 5); err == nil {
		t.Fatal("expected error when vector arm fails, got nil")
	}
	// Fail-fast: keyword arm should not have been reached.
	if st.lastKeywordK != 0 {
		t.Errorf("keyword arm was called (lastKeywordK=%d) after vector arm failure, want 0", st.lastKeywordK)
	}
}

func TestHybridSearch_KeywordArmErrorFailsFast(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{keywordErr: errors.New("db down")}
	r := New(emb, st)

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 5); err == nil {
		t.Fatal("expected error when keyword arm fails, got nil")
	}
}

func TestHybridSearch_NoReranker_UnchangedBehavior(t *testing.T) {
	// New() without WithReranker must behave exactly like before this stage
	// existed: fusion alone produces the final topK, no rerank call.
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches:        []store.Match{{ChunkID: 1, Content: "vector top"}},
		keywordMatches: []store.Match{{ChunkID: 2, Content: "keyword only"}},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "lừa đảo", 2)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (fusion-only, no reranker configured)", len(results))
	}
}

func TestHybridSearch_RerankerReceivesFusedCandidatesInOrder(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{ChunkID: 1, Content: "vec1"},
			{ChunkID: 2, Content: "vec2"},
		},
		keywordMatches: []store.Match{
			{ChunkID: 3, Content: "kw3"},
		},
	}
	rr := &fakeReranker{}
	r := New(emb, st, WithReranker(rr))

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 2); err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	if !rr.called {
		t.Fatal("reranker was not called")
	}
	if rr.lastQuery != "lừa đảo" {
		t.Errorf("reranker query = %q, want %q", rr.lastQuery, "lừa đảo")
	}
	// Fused order (RRF): chunk 1 and chunk 3 tie at rank 0 of their arm (tie
	// broken by ChunkID asc), chunk 2 is rank 1 of the vector arm.
	wantDocs := []string{"vec1", "kw3", "vec2"}
	if len(rr.lastDocs) != len(wantDocs) {
		t.Fatalf("reranker got %d docs, want %d", len(rr.lastDocs), len(wantDocs))
	}
	for i, want := range wantDocs {
		if rr.lastDocs[i] != want {
			t.Errorf("reranker docs[%d] = %q, want %q", i, rr.lastDocs[i], want)
		}
	}
	if rr.lastTopK != 2 {
		t.Errorf("reranker topK = %d, want 2 (the caller's final topK)", rr.lastTopK)
	}
}

func TestHybridSearch_RerankerReordersAndSetsScore(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{ChunkID: 1, Content: "vec1"},
			{ChunkID: 2, Content: "vec2"},
		},
		keywordMatches: []store.Match{
			{ChunkID: 3, Content: "kw3"},
		},
	}
	// Fused order is [chunk1, chunk3, chunk2] (index 0,1,2). The reranker
	// prefers chunk2 (index 2) over chunk1 (index 0), dropping chunk3.
	rr := &fakeReranker{results: []reranker.Result{
		{Index: 2, RelevanceScore: 0.9},
		{Index: 0, RelevanceScore: 0.5},
	}}
	r := New(emb, st, WithReranker(rr))

	results, err := r.HybridSearch(context.Background(), "lừa đảo", 2)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ChunkID != 2 || results[0].Score != 0.9 {
		t.Errorf("results[0] = {ChunkID:%d Score:%v}, want {ChunkID:2 Score:0.9}", results[0].ChunkID, results[0].Score)
	}
	if results[1].ChunkID != 1 || results[1].Score != 0.5 {
		t.Errorf("results[1] = {ChunkID:%d Score:%v}, want {ChunkID:1 Score:0.5}", results[1].ChunkID, results[1].Score)
	}
}

func TestHybridSearch_RerankErrorPropagates(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{matches: []store.Match{{ChunkID: 1, Content: "vec1"}}}
	rr := &fakeReranker{err: errors.New("rerank api down")}
	r := New(emb, st, WithReranker(rr))

	if _, err := r.HybridSearch(context.Background(), "lừa đảo", 2); err == nil {
		t.Fatal("expected rerank error to propagate, got nil")
	}
}

func TestHybridSearch_EmptyFusionSkipsRerankCall(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{} // no matches on either arm
	rr := &fakeReranker{}
	r := New(emb, st, WithReranker(rr))

	results, err := r.HybridSearch(context.Background(), "lừa đảo", 5)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
	if rr.called {
		t.Error("reranker was called despite empty fusion")
	}
}
