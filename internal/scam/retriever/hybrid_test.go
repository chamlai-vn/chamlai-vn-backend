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
	// Document 1 ranks in both arms, document 2 only in vector, document 3
	// only in keyword. The document both arms agree on should come out on top.
	vector := []store.Match{
		{DocumentID: 2, DocumentContent: "vector only"},
		{DocumentID: 1, DocumentContent: "in both"},
	}
	keyword := []store.Match{
		{DocumentID: 1, DocumentContent: "in both"},
		{DocumentID: 3, DocumentContent: "keyword only"},
	}

	out := reciprocalRankFusion(vector, keyword, 3)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3", len(out))
	}
	if out[0].DocumentID != 1 {
		t.Errorf("out[0].DocumentID = %d, want 1 (present in both arms)", out[0].DocumentID)
	}
}

func TestReciprocalRankFusion_Dedup(t *testing.T) {
	// Same document in both arms must appear once in the output, not twice.
	vector := []store.Match{{DocumentID: 1, DocumentContent: "x"}}
	keyword := []store.Match{{DocumentID: 1, DocumentContent: "x"}}

	out := reciprocalRankFusion(vector, keyword, 10)
	if len(out) != 1 {
		t.Fatalf("got %d results, want 1 (deduped)", len(out))
	}
}

func TestReciprocalRankFusion_TopKTruncates(t *testing.T) {
	vector := []store.Match{
		{DocumentID: 1}, {DocumentID: 2}, {DocumentID: 3}, {DocumentID: 4},
	}
	out := reciprocalRankFusion(vector, nil, 2)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (topK truncation)", len(out))
	}
	if out[0].DocumentID != 1 || out[1].DocumentID != 2 {
		t.Errorf("got DocumentIDs %d,%d, want 1,2 (vector rank order preserved)", out[0].DocumentID, out[1].DocumentID)
	}
}

func TestReciprocalRankFusion_EmptyKeywordArmPreservesVectorOrder(t *testing.T) {
	vector := []store.Match{{DocumentID: 5}, {DocumentID: 1}, {DocumentID: 9}}
	out := reciprocalRankFusion(vector, nil, 3)
	if len(out) != 3 {
		t.Fatalf("got %d results, want 3", len(out))
	}
	want := []int64{5, 1, 9}
	for i, id := range want {
		if out[i].DocumentID != id {
			t.Errorf("out[%d].DocumentID = %d, want %d", i, out[i].DocumentID, id)
		}
	}
}

func TestReciprocalRankFusion_DeterministicTieBreak(t *testing.T) {
	// Two documents tied at rank 0 of separate, non-overlapping arms get equal
	// fused scores; output order must be deterministic (DocumentID ascending).
	vector := []store.Match{{DocumentID: 20}}
	keyword := []store.Match{{DocumentID: 10}}

	out := reciprocalRankFusion(vector, keyword, 2)
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2", len(out))
	}
	if out[0].DocumentID != 10 || out[1].DocumentID != 20 {
		t.Errorf("got DocumentIDs %d,%d, want 10,20 (tie broken by DocumentID asc)", out[0].DocumentID, out[1].DocumentID)
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
			{DocumentID: 1, DocumentContent: "vector top"},
			{DocumentID: 2, DocumentContent: "vector second"},
		},
		keywordMatches: []store.Match{
			{DocumentID: 1, DocumentContent: "vector top"},
			{DocumentID: 3, DocumentContent: "keyword only"},
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
	if results[0].DocumentID != 1 {
		t.Errorf("results[0].DocumentID = %d, want 1 (agreed by both arms)", results[0].DocumentID)
	}
}

func TestHybridSearch_PerArmCollapseAvoidsCountDomination(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		// Document 1 has 3 vectors (content chunk + two doc2query lines) that
		// all matched. Without per-arm collapse before fusion, the vector arm
		// alone would sum 3 RRF contributions for doc 1, inflating its score
		// far above what a single arm's agreement should award.
		matches: []store.Match{
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 1, DocumentContent: "doc1"},
		},
		keywordMatches: []store.Match{
			{DocumentID: 2, DocumentContent: "doc2"},
		},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 distinct documents", len(results))
	}

	var doc1Score float64
	for _, res := range results {
		if res.DocumentID == 1 {
			doc1Score = res.Score
		}
	}
	wantDoc1Score := 1.0 / float64(rrfK+1) // one arm, rank 0, one contribution
	if doc1Score != wantDoc1Score {
		t.Errorf("doc1 score = %v, want %v (one arm's best-rank contribution, not summed across its 3 vectors)", doc1Score, wantDoc1Score)
	}
}

func TestHybridSearch_ResultContentIsDocumentBodyNotMatchedChunk(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{
				DocumentID:      1,
				Content:         "mình nhận tin nhắn trúng thưởng, có phải lừa đảo không?",
				DocumentContent: "Nội dung cảnh báo đầy đủ.",
			},
		},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "trúng thưởng lừa đảo", 5)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Content != "Nội dung cảnh báo đầy đủ." {
		t.Errorf("Content = %q, want the document body, not the matched (query-kind) chunk text", results[0].Content)
	}
}

func TestHybridSearch_OverfetchAvoidsStarvationWhenOneDocumentDominates(t *testing.T) {
	// Same regression shape as the Retrieve version: a single dominating
	// document must not starve the fused result of other distinct documents.
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 1, DocumentContent: "doc1"},
			{DocumentID: 2, DocumentContent: "doc2"},
			{DocumentID: 3, DocumentContent: "doc3"},
			{DocumentID: 4, DocumentContent: "doc4"},
			{DocumentID: 5, DocumentContent: "doc5"},
		},
	}
	r := New(emb, st)

	results, err := r.HybridSearch(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5 distinct documents despite doc 1 dominating the first 4 raw ranks", len(results))
	}
	seen := make(map[int64]bool)
	for _, res := range results {
		seen[res.DocumentID] = true
	}
	for _, id := range []int64{1, 2, 3, 4, 5} {
		if !seen[id] {
			t.Errorf("document %d missing from results", id)
		}
	}
}

func TestHybridSearch_DefaultTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{DocumentID: 1}, {DocumentID: 2}, {DocumentID: 3}, {DocumentID: 4}, {DocumentID: 5}, {DocumentID: 6},
		},
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
		matches:        []store.Match{{DocumentID: 1, DocumentContent: "vector top"}},
		keywordMatches: []store.Match{{DocumentID: 2, DocumentContent: "keyword only"}},
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
			{DocumentID: 1, DocumentContent: "vec1"},
			{DocumentID: 2, DocumentContent: "vec2"},
		},
		keywordMatches: []store.Match{
			{DocumentID: 3, DocumentContent: "kw3"},
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
	// Fused order (RRF): document 1 and document 3 tie at rank 0 of their arm
	// (tie broken by DocumentID asc), document 2 is rank 1 of the vector arm.
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
			{DocumentID: 1, DocumentContent: "vec1"},
			{DocumentID: 2, DocumentContent: "vec2"},
		},
		keywordMatches: []store.Match{
			{DocumentID: 3, DocumentContent: "kw3"},
		},
	}
	// Fused order is [doc1, doc3, doc2] (index 0,1,2). The reranker prefers
	// doc2 (index 2) over doc1 (index 0), dropping doc3.
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
	if results[0].DocumentID != 2 || results[0].Score != 0.9 {
		t.Errorf("results[0] = {DocumentID:%d Score:%v}, want {DocumentID:2 Score:0.9}", results[0].DocumentID, results[0].Score)
	}
	if results[1].DocumentID != 1 || results[1].Score != 0.5 {
		t.Errorf("results[1] = {DocumentID:%d Score:%v}, want {DocumentID:1 Score:0.5}", results[1].DocumentID, results[1].Score)
	}
}

func TestHybridSearch_RerankErrorPropagates(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{matches: []store.Match{{DocumentID: 1, DocumentContent: "vec1"}}}
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
