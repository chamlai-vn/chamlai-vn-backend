package retriever

import (
	"context"
	"errors"
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
			{ChunkID: 1, DocumentID: 10, Content: "scam A", ScamType: "lừa-đảo", SourceURL: "https://a.vn", Distance: 0.2},
			{ChunkID: 2, DocumentID: 11, Content: "scam B", ScamType: "lừa-đảo", SourceURL: "https://b.vn", Distance: 1.1},
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
	if results[0].ChunkID != 1 || results[0].DocumentID != 10 {
		t.Errorf("result[0] ids: ChunkID=%d DocumentID=%d, want 1/10", results[0].ChunkID, results[0].DocumentID)
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

func TestRetrieve_DefaultTopK(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}

	// topK=0 → default 5
	r := New(emb, st)
	_, _ = r.Retrieve(context.Background(), "test", 0)
	if st.lastK != 5 {
		t.Errorf("lastK = %d, want 5 (default)", st.lastK)
	}

	// WithDefaultTopK(3) overrides
	r2 := New(emb, st, WithDefaultTopK(3))
	_, _ = r2.Retrieve(context.Background(), "test", 0)
	if st.lastK != 3 {
		t.Errorf("lastK = %d, want 3 (from WithDefaultTopK)", st.lastK)
	}

	// Explicit positive topK is passed through
	_, _ = r.Retrieve(context.Background(), "test", 7)
	if st.lastK != 7 {
		t.Errorf("lastK = %d, want 7 (explicit)", st.lastK)
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
