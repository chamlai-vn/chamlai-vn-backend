// Package ingest is the indexing-side use case: it turns a raw scam-warning
// document into stored, retrievable chunks. It wires together the three pieces
// that previously only existed apart:
//
//	doc text → ragutil.Chunk() → embed each chunk (batched) → store atomically
//
// It is the indexing counterpart to internal/analyzer (the query side). Both
// the corpus crawler (cmd/crawler) and manual seeding (cmd/seed) drive the
// corpus through this one path, so chunking and embedding stay consistent
// between what we index and what we later search.
//
// Construction lives in service.go (Indexer, New, Options); request/result
// types in type.go; the pipeline behaviour is here.
package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/store"
	ragutil "github.com/chamlai-vn/chamlai-vn-backend/pkg/util/rag"
)

// IndexDocument runs the full pipeline for one document: split it into chunks,
// embed every chunk as a corpus document, and store the document with its
// chunks atomically.
//
// Embedding happens before any DB write, so a provider failure leaves nothing
// behind. Empty/whitespace-only content is rejected up front — ragutil.Chunk
// would otherwise hand back the blank text as a single chunk, and there is
// nothing to retrieve from it.
func (ix *Indexer) IndexDocument(ctx context.Context, doc Document) (Result, error) {
	if strings.TrimSpace(doc.Content) == "" {
		return Result{}, fmt.Errorf("ingest: %q has no indexable content", doc.URL)
	}

	texts := ragutil.Chunk(doc.Content, ix.chunkCfg)
	if len(texts) == 0 {
		return Result{}, fmt.Errorf("ingest: %q produced no chunks", doc.URL)
	}

	vectors, err := ix.embedChunks(ctx, texts)
	if err != nil {
		return Result{}, err
	}

	chunks := make([]store.Chunk, len(texts))
	for i, t := range texts {
		chunks[i] = store.Chunk{Content: t, Embedding: vectors[i]}
	}

	docID, err := ix.store.InsertDocumentWithChunks(
		ctx, doc.URL, doc.Title, doc.Content, doc.ScamType, doc.Source, chunks)
	if err != nil {
		return Result{}, err
	}
	return Result{DocID: docID, Chunks: len(chunks)}, nil
}

// embedChunks embeds texts as corpus documents, splitting into batches of at
// most ix.batchSize so a large document can't exceed the provider's per-call
// input limit. Vectors are returned in the same order as texts.
func (ix *Indexer) embedChunks(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += ix.batchSize {
		end := start + ix.batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		vecs, err := ix.emb.Embed(ctx, batch, embedder.InputDocument)
		if err != nil {
			return nil, fmt.Errorf("ingest: embed chunks [%d:%d]: %w", start, end, err)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("ingest: embed returned %d vectors for %d chunks", len(vecs), len(batch))
		}
		vectors = append(vectors, vecs...)
	}
	return vectors, nil
}
