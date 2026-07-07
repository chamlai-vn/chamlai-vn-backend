package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
	ragutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/rag"
)

// fakeEmbedder returns one deterministic vector per input and records every
// batch it was asked to embed, so tests can assert on batching, input type,
// and (via vectorFor) control which texts embed near-identically.
type fakeEmbedder struct {
	dims      int
	calls     [][]string
	lastInput embedder.InputType
	err       error
	// vectorFor, if set, computes the vector for a text (matched by
	// substring so tests don't need to know the exact contextual-prefixed
	// string). Falls back to a zero vector of length dims.
	vectorFor func(text string) []float32
}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string, it embedder.InputType) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, append([]string(nil), texts...))
	f.lastInput = it
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if f.vectorFor != nil {
			out[i] = f.vectorFor(t)
			continue
		}
		out[i] = make([]float32, f.dims) // zero vector of the right size is enough
	}
	return out, nil
}

func (f *fakeEmbedder) Dimensions() int { return f.dims }
func (f *fakeEmbedder) Model() string   { return "fake" }

// fakeStore records what it was asked to persist.
type fakeStore struct {
	doc    model.Document
	chunks []model.Chunk
	calls  int
	err    error
}

func (f *fakeStore) InsertDocumentWithChunks(ctx context.Context, doc model.Document, chunks []model.Chunk) (int64, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	f.doc = doc
	f.chunks = chunks
	return 42, nil
}

func TestIndexDocument_ChunksEmbedsAndStores(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	// Force multiple content chunks from a long body, and a batch size that forces a split.
	body := strings.Repeat("Cảnh báo lừa đảo. ", 500)
	ix := New(emb, st,
		WithChunkConfig(ragutil.ChunkConfig{Size: 200, Overlap: 20}),
		WithBatchSize(2),
	)

	res, err := ix.IndexDocument(context.Background(), corpusdoc.Document{
		URL:      "https://example.invalid/a",
		Title:    "t",
		Content:  body,
		ScamType: "test",
	})
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// chunkContent trims the body before splitting on paragraph boundaries, so
	// the expectation must be computed the same way.
	wantChunks := ragutil.Chunk(strings.TrimSpace(body), ragutil.ChunkConfig{Size: 200, Overlap: 20})
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

	// Chunk content, kind, and order must be preserved through the pipeline.
	for i, c := range st.chunks {
		if c.Content != wantChunks[i] {
			t.Errorf("chunk %d content mismatch", i)
		}
		if c.Kind != model.ChunkKindContent {
			t.Errorf("chunk %d kind = %q, want %q", i, c.Kind, model.ChunkKindContent)
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

func TestIndexDocument_QueryChunksGetKindAndContextualPrefix(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	ix := New(emb, st)

	doc := corpusdoc.Document{
		URL:      "https://example.invalid/b",
		Title:    "Mạo danh công an",
		Content:  "Đối tượng giả danh công an gọi điện đe dọa nạn nhân.",
		ScamType: "impersonation_authority",
		UserQueries: []string{
			"Có người tự xưng công an gọi điện đòi chuyển tiền, có phải lừa đảo không?",
			"Nhận được lệnh bắt giả qua Zalo, phải làm sao?",
		},
	}

	if _, err := ix.IndexDocument(context.Background(), doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	var contentChunks, queryChunks []model.Chunk
	for _, c := range st.chunks {
		switch c.Kind {
		case model.ChunkKindContent:
			contentChunks = append(contentChunks, c)
		case model.ChunkKindQuery:
			queryChunks = append(queryChunks, c)
		default:
			t.Errorf("unexpected chunk kind %q", c.Kind)
		}
	}
	if len(contentChunks) != 1 {
		t.Errorf("content chunks = %d, want 1", len(contentChunks))
	}
	if len(queryChunks) != len(doc.UserQueries) {
		t.Fatalf("query chunks = %d, want %d", len(queryChunks), len(doc.UserQueries))
	}
	for i, c := range queryChunks {
		if c.Content != doc.UserQueries[i] {
			t.Errorf("query chunk %d stored content = %q, want exact (unprefixed) %q", i, c.Content, doc.UserQueries[i])
		}
	}

	// The contextual prefix (title + scam type) must reach the embedder for
	// every chunk...
	wantPrefix := contextualPrefix(doc.Title, doc.ScamType)
	var embeddedTexts []string
	for _, call := range emb.calls {
		embeddedTexts = append(embeddedTexts, call...)
	}
	if len(embeddedTexts) != len(contentChunks)+len(queryChunks) {
		t.Fatalf("embedded %d texts, want %d", len(embeddedTexts), len(contentChunks)+len(queryChunks))
	}
	for _, text := range embeddedTexts {
		if !strings.HasPrefix(text, wantPrefix) {
			t.Errorf("embedded text %q does not start with contextual prefix %q", text, wantPrefix)
		}
	}
	// ...but the prefix must NOT leak into the stored content (content_tsv's
	// source), which is what makes it "cheap" instead of diluting BM25.
	for _, c := range st.chunks {
		if strings.Contains(c.Content, doc.Title) {
			t.Errorf("stored chunk content %q leaked the contextual prefix", c.Content)
		}
	}
}

func TestIndexDocument_BlankUserQueryLinesAreSkipped(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	ix := New(emb, st)

	_, err := ix.IndexDocument(context.Background(), corpusdoc.Document{
		URL:         "https://example.invalid/c",
		Title:       "t",
		Content:     "nội dung",
		ScamType:    "other",
		UserQueries: []string{"câu hỏi thật", "   ", ""},
	})
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	var queryChunks int
	for _, c := range st.chunks {
		if c.Kind == model.ChunkKindQuery {
			queryChunks++
		}
	}
	if queryChunks != 1 {
		t.Errorf("query chunks = %d, want 1 (blank lines must be dropped)", queryChunks)
	}
}

func TestIndexDocument_NearDuplicateQueriesAreDropped(t *testing.T) {
	emb := &fakeEmbedder{
		dims: 4,
		vectorFor: func(text string) []float32 {
			switch {
			case strings.Contains(text, "câu hỏi gốc"):
				return []float32{1, 0, 0, 0}
			case strings.Contains(text, "câu hỏi gần giống"):
				return []float32{1, 0, 0, 0} // identical vector => cosine similarity 1.0 => near-dup
			case strings.Contains(text, "câu hỏi khác hẳn"):
				return []float32{0, 1, 0, 0} // orthogonal => not a dup
			default:
				return []float32{0, 0, 1, 0} // content chunk
			}
		},
	}
	st := &fakeStore{}
	ix := New(emb, st)

	_, err := ix.IndexDocument(context.Background(), corpusdoc.Document{
		URL:      "https://example.invalid/d",
		Title:    "t",
		Content:  "nội dung cảnh báo",
		ScamType: "other",
		UserQueries: []string{
			"đây là câu hỏi gốc của nạn nhân",
			"đây là câu hỏi gần giống câu trên",
			"đây là câu hỏi khác hẳn về chủ đề khác",
		},
	})
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	var kept []string
	for _, c := range st.chunks {
		if c.Kind == model.ChunkKindQuery {
			kept = append(kept, c.Content)
		}
	}
	if len(kept) != 2 {
		t.Fatalf("kept %d query chunks, want 2 (one near-duplicate should be dropped): %#v", len(kept), kept)
	}
	if strings.Contains(kept[0], "gần giống") || (len(kept) > 1 && strings.Contains(kept[1], "gần giống")) {
		t.Errorf("near-duplicate query text should have been dropped, kept = %#v", kept)
	}
}

func TestIndexDocument_EmptyContentRejected(t *testing.T) {
	emb := &fakeEmbedder{dims: 4}
	st := &fakeStore{}
	ix := New(emb, st)

	_, err := ix.IndexDocument(context.Background(), corpusdoc.Document{URL: "u", Content: "   \n\t "})
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

	_, err := ix.IndexDocument(context.Background(), corpusdoc.Document{URL: "u", Content: "some scam text"})
	if err == nil {
		t.Fatal("expected embed error, got nil")
	}
	if st.calls != 0 {
		t.Errorf("store must not be called when embedding fails; got %d calls", st.calls)
	}
}
