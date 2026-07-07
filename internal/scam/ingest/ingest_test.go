package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
	ragutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/rag"
)

// fakeEmbedder returns one deterministic vector per input and records every
// batch it was asked to embed, so tests can assert on batching and input type.
type fakeEmbedder struct {
	dims      int
	calls     [][]string
	lastInput embedder.InputType
	err       error
}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string, it embedder.InputType) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, append([]string(nil), texts...))
	f.lastInput = it
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, f.dims) // zero vector of the right size is enough
	}
	return out, nil
}

func (f *fakeEmbedder) Dimensions() int { return f.dims }
func (f *fakeEmbedder) Model() string   { return "fake" }

// fakeStore records what it was asked to persist.
type fakeStore struct {
	chunks []model.Chunk
	calls  int
	err    error
}

func (f *fakeStore) InsertDocumentWithChunks(ctx context.Context, doc model.Document, chunks []model.Chunk) (int64, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	f.chunks = chunks
	return 42, nil
}

func TestIndexDocument_ChunksEmbedsAndStores(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	// Force multiple chunks from a long body, and a batch size that forces a split.
	body := strings.Repeat("Cảnh báo lừa đảo. ", 500)
	ix := New(emb, st,
		WithChunkConfig(ragutil.ChunkConfig{Size: 200, Overlap: 20}),
		WithBatchSize(2),
	)

	res, err := ix.IndexDocument(context.Background(), Document{
		URL:      "https://example.invalid/a",
		Title:    "t",
		Content:  body,
		ScamType: "test",
		Source:   "unit",
	})
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	wantChunks := ragutil.Chunk(body, ragutil.ChunkConfig{Size: 200, Overlap: 20})
	if len(wantChunks) < 2 {
		t.Fatalf("test setup: expected multiple chunks, got %d", len(wantChunks))
	}

	if res.DocID != 42 {
		t.Errorf("DocID = %d, want 42", res.DocID)
	}
	if res.Chunks != len(wantChunks) {
		t.Errorf("Result.Chunks = %d, want %d", res.Chunks, len(wantChunks))
	}
	if len(st.chunks) != len(wantChunks) {
		t.Errorf("stored %d chunks, want %d", len(st.chunks), len(wantChunks))
	}

	// Chunk content and order must be preserved through the pipeline.
	for i, c := range st.chunks {
		if c.Content != wantChunks[i] {
			t.Errorf("chunk %d content mismatch", i)
		}
		if len(c.Embedding) != emb.dims {
			t.Errorf("chunk %d embedding dims = %d, want %d", i, len(c.Embedding), emb.dims)
		}
	}

	// Corpus must be embedded as documents, not queries.
	if emb.lastInput != embedder.InputDocument {
		t.Errorf("input type = %q, want %q", emb.lastInput, embedder.InputDocument)
	}

	// Batching: with batch size 2, each call carries at most 2 texts.
	for i, call := range emb.calls {
		if len(call) > 2 {
			t.Errorf("call %d had %d texts, want <= 2", i, len(call))
		}
	}
}

func TestIndexDocument_EmptyContentRejected(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	ix := New(emb, st)

	_, err := ix.IndexDocument(context.Background(), Document{URL: "u", Content: "   \n\t "})
	if err == nil {
		t.Fatal("expected error for whitespace-only content, got nil")
	}
	if st.calls != 0 {
		t.Errorf("store was called %d times, want 0", st.calls)
	}
}

func TestIndexDocument_EmbedFailureSkipsStore(t *testing.T) {
	emb := &fakeEmbedder{dims: 4, err: errors.New("boom")}
	st := &fakeStore{}
	ix := New(emb, st)

	_, err := ix.IndexDocument(context.Background(), Document{URL: "u", Content: "some scam text"})
	if err == nil {
		t.Fatal("expected embed error, got nil")
	}
	if st.calls != 0 {
		t.Errorf("store must not be called when embedding fails; got %d calls", st.calls)
	}
}
