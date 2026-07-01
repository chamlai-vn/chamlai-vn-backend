package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

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
