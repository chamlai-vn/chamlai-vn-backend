package retriever

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// fakeEmbedder returns one deterministic zero-vector per input and records
// the last InputType it was called with, so tests can assert on query intent.
type fakeEmbedder struct {
	dims      int
	lastInput embedder.InputType
	err       error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string, it embedder.InputType) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastInput = it
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, f.dims)
	}
	return out, nil
}

func (f *fakeEmbedder) Dimensions() int { return f.dims }
func (f *fakeEmbedder) Model() string   { return "fake" }

// fakeStore records the k it was called with and returns canned matches. It
// implements both retriever.Store arms; keywordMatches/lastKeywordK/keywordErr
// let tests drive the keyword arm independently of the vector arm.
type fakeStore struct {
	matches []store.Match
	lastK   int
	err     error

	keywordMatches []store.Match
	lastKeywordK   int
	lastKeywordQ   string
	keywordErr     error
}

func (f *fakeStore) SearchSimilar(_ context.Context, _ []float32, k int) ([]store.Match, error) {
	f.lastK = k
	if f.err != nil {
		return nil, f.err
	}
	return f.matches, nil
}

func (f *fakeStore) SearchByKeyword(_ context.Context, query string, k int) ([]store.Match, error) {
	f.lastKeywordQ = query
	f.lastKeywordK = k
	if f.keywordErr != nil {
		return nil, f.keywordErr
	}
	return f.keywordMatches, nil
}

func TestRetrieve_EmbedsAsQuery(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	r := New(emb, st)

	if _, err := r.Retrieve(context.Background(), "lừa đảo", 3); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if emb.lastInput != embedder.InputQuery {
		t.Errorf("input type = %q, want %q", emb.lastInput, embedder.InputQuery)
	}
}

func TestRetrieve_MapsAndScores(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			{DocumentID: 10, DocumentContent: "scam A", ScamType: "lừa-đảo", SourceURL: "https://a.vn", Distance: 0.2},
			{DocumentID: 11, DocumentContent: "scam B", ScamType: "lừa-đảo", SourceURL: "https://b.vn", Distance: 1.1},
		},
	}
	r := New(emb, st)

	results, err := r.Retrieve(context.Background(), "nghi ngờ", 2)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// distance 0.2 → score 0.8
	if results[0].DocumentID != 10 {
		t.Errorf("result[0].DocumentID = %d, want 10", results[0].DocumentID)
	}
	if results[0].Content != "scam A" {
		t.Errorf("result[0].Content = %q, want %q (document body, from DocumentContent)", results[0].Content, "scam A")
	}
	if results[0].Score != 0.8 {
		t.Errorf("result[0].Score = %f, want 0.8", results[0].Score)
	}
	if results[0].SourceURL != "https://a.vn" {
		t.Errorf("result[0].SourceURL = %q, want https://a.vn", results[0].SourceURL)
	}

	// distance 1.1 → 1 - 1.1 = -0.1, clamped to 0
	if results[1].Score != 0 {
		t.Errorf("result[1].Score = %f, want 0 (clamped from -0.1)", results[1].Score)
	}
}

func TestRetrieve_FetchesCandidateTopKRegardlessOfTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	r := New(emb, st)

	// The store fetch (candidateTopK) is decoupled from the caller's topK —
	// it's the overfetch that makes post-collapse results reliable. See
	// TestRetrieve_FetchesCandidateTopKAndTruncatesToTopK for the truncation
	// side of this.
	for _, topK := range []int{0, 3, 7} {
		st.lastK = 0
		if _, err := r.Retrieve(context.Background(), "test", topK); err != nil {
			t.Fatalf("Retrieve(topK=%d): %v", topK, err)
		}
		if st.lastK != candidateTopK {
			t.Errorf("topK=%d: store fetched k=%d, want candidateTopK=%d", topK, st.lastK, candidateTopK)
		}
	}
}

func TestRetrieve_FetchesCandidateTopKAndTruncatesToTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	var matches []store.Match
	for i := int64(1); i <= 6; i++ {
		matches = append(matches, store.Match{DocumentID: i, DocumentContent: fmt.Sprintf("doc %d", i), Distance: float64(i) * 0.1})
	}
	st := &fakeStore{matches: matches}

	// Default topK (5).
	r := New(emb, st)
	results, err := r.Retrieve(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("got %d results, want 5 (default topK, after overfetch+collapse+truncate)", len(results))
	}

	// WithDefaultTopK(3) changes the truncation, not the fetch.
	r2 := New(emb, st, WithDefaultTopK(3))
	results2, err := r2.Retrieve(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results2) != 3 {
		t.Errorf("got %d results, want 3 (WithDefaultTopK)", len(results2))
	}

	// Explicit positive topK is honored.
	results3, err := r.Retrieve(context.Background(), "test", 2)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results3) != 2 {
		t.Errorf("got %d results, want 2 (explicit topK)", len(results3))
	}
}

func TestRetrieve_CollapsesMultiChunkSameDocumentToSingleResult(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			// Three vectors of the SAME document (its content chunk plus two
			// doc2query "# User query" lines) — the classic multi-
			// representation shape. The best-ranked (first) one must win.
			{DocumentID: 1, Content: "content chunk", DocumentContent: "full doc body", Distance: 0.1},
			{DocumentID: 1, Content: "câu hỏi 1", DocumentContent: "full doc body", Distance: 0.15},
			{DocumentID: 1, Content: "câu hỏi 2", DocumentContent: "full doc body", Distance: 0.2},
			{DocumentID: 2, Content: "other doc", DocumentContent: "other doc body", Distance: 0.3},
		},
	}
	r := New(emb, st)

	results, err := r.Retrieve(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 distinct documents (3 chunks of doc 1 must collapse to 1)", len(results))
	}
	if results[0].DocumentID != 1 || results[0].Score != scoreFromDistance(0.1) {
		t.Errorf("results[0] = {DocumentID:%d Score:%v}, want the best-ranked (distance 0.1) chunk of doc 1", results[0].DocumentID, results[0].Score)
	}
}

func TestRetrieve_ContentIsDocumentBodyNotMatchedChunk(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{
		matches: []store.Match{
			// A query-vector match: the chunk that matched is a victim-style
			// question, but grounding must be the document's own content.
			{
				DocumentID:      1,
				Content:         "mình nhận tin nhắn báo trúng thưởng, có phải lừa đảo không?",
				DocumentContent: "Nội dung cảnh báo lừa đảo trúng thưởng đầy đủ.",
			},
		},
	}
	r := New(emb, st)

	results, err := r.Retrieve(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Content != "Nội dung cảnh báo lừa đảo trúng thưởng đầy đủ." {
		t.Errorf("Content = %q, want the document body, not the matched (query-kind) chunk text", results[0].Content)
	}
}

// TestRetrieve_OverfetchAvoidsStarvationWhenOneDocumentDominates is a
// regression test for the bug the deepen-plan review caught: at the old
// candidateTopK (20, ~= a naive fetch of just topK), a single document
// occupying the first several ranks could starve the result of other
// distinct documents entirely. Fetching the full (larger) candidate set
// before collapsing — rather than collapsing only the first topK raw
// matches — is what fixes it.
func TestRetrieve_OverfetchAvoidsStarvationWhenOneDocumentDominates(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}

	// Document 1 dominates the first 4 ranks (e.g. its content chunk plus 3
	// query chunks all matched strongly); 5 other distinct documents follow.
	matches := []store.Match{
		{DocumentID: 1, DocumentContent: "doc1"},
		{DocumentID: 1, DocumentContent: "doc1"},
		{DocumentID: 1, DocumentContent: "doc1"},
		{DocumentID: 1, DocumentContent: "doc1"},
		{DocumentID: 2, DocumentContent: "doc2"},
		{DocumentID: 3, DocumentContent: "doc3"},
		{DocumentID: 4, DocumentContent: "doc4"},
		{DocumentID: 5, DocumentContent: "doc5"},
		{DocumentID: 6, DocumentContent: "doc6"},
	}
	st := &fakeStore{matches: matches}
	r := New(emb, st)

	results, err := r.Retrieve(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("got %d results, want 5 distinct documents despite doc 1 dominating the first 4 raw ranks", len(results))
	}
	seen := make(map[int64]bool)
	for _, res := range results {
		if seen[res.DocumentID] {
			t.Errorf("document %d appeared more than once in results", res.DocumentID)
		}
		seen[res.DocumentID] = true
	}
	if !seen[1] {
		t.Error("dominating document 1 should still appear once")
	}
	for _, id := range []int64{2, 3, 4, 5} {
		if !seen[id] {
			t.Errorf("document %d was starved out despite being in the candidate set", id)
		}
	}
}

func TestRetrieve_RejectsEmptyAndOverLong(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	r := New(emb, st)
	ctx := context.Background()

	cases := []struct {
		name  string
		query string
	}{
		{"empty string", ""},
		{"whitespace only", "   \n\t "},
		{"5001 ASCII runes", strings.Repeat("a", 5001)},
	}
	for _, tc := range cases {
		st.lastK = 0
		_, err := r.Retrieve(ctx, tc.query, 5)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if st.lastK != 0 {
			t.Errorf("%s: store was called (lastK=%d), want 0", tc.name, st.lastK)
		}
	}

	// Exactly 5000 runes of multibyte Vietnamese must be accepted.
	// 'ắ' is 3 bytes in UTF-8 — guards byte-vs-rune regression.
	exactly5000 := strings.Repeat("ắ", 5000)
	if utf8.RuneCountInString(exactly5000) != 5000 {
		t.Fatal("test setup: expected 5000 runes")
	}
	if _, err := r.Retrieve(ctx, exactly5000, 1); err != nil {
		t.Errorf("5000-rune Vietnamese query was rejected: %v", err)
	}
}

func TestRetrieve_EmptyMatches(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{matches: nil}
	r := New(emb, st)

	results, err := r.Retrieve(context.Background(), "something", 5)
	if err != nil {
		t.Fatalf("Retrieve with empty corpus: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestRetrieve_EmbedFailureSkipsSearch(t *testing.T) {
	emb := &fakeEmbedder{dims: 4, err: errors.New("voyage down")}
	st := &fakeStore{}
	r := New(emb, st)

	if _, err := r.Retrieve(context.Background(), "lừa đảo", 5); err == nil {
		t.Fatal("expected embed error, got nil")
	}
	if st.lastK != 0 {
		t.Errorf("store was called (lastK=%d) after embed failure, want 0", st.lastK)
	}
}
