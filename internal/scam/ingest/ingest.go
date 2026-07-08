// Package ingest is the indexing-side use case: it turns a parsed
// corpusdoc.Document into stored, retrievable, multi-representation chunks.
// It wires together the pieces that previously only existed apart:
//
//	Content → one chunk, whole (kind=content)
//	User query lines → one chunk each, doc2query (kind=query)
//	→ embed everything (contextual-prefixed) → store atomically
//
// Content is stored whole rather than sub-split: corpus documents are
// already short, single-topic summaries produced by internal/scam/enrich, so
// splitting on paragraph/size boundaries only fragmented a coherent scam
// narrative across multiple chunks without any corresponding retrieval
// benefit (Voyage's context window is far larger than these documents ever
// get).
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
	"math"
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/model"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

// sourceCorpus is the fixed documents.source value for every document
// ingested through the structured corpus pipeline. The old per-crawl-site
// source key (e.g. "vnexpress") no longer distinguishes anything once
// crawl→enrich→review funnels every document through the same canonical
// corpusdoc format.
const sourceCorpus = "corpus"

// nearDupCosineThreshold: a query chunk whose embedding is at least this
// cosine-similar to an already-kept query chunk of the same document is
// dropped as a near-duplicate — insurance against a reviewer leaving two
// near-identical paraphrases in "# User query" (see the plan's doc2query
// research notes). Set high (not e.g. 0.9) because "# User query" lines are
// deliberately generated to cover distinct victim intents that legitimately
// share vocabulary (same scam scenario) — a lower threshold would discard
// genuine intent diversity, not just accidental duplicates.
const nearDupCosineThreshold = 0.95

// IndexDocument runs the full multi-representation pipeline for one
// corpusdoc.Document: split Content into sub-chunks (kind=content), turn
// each User query line into its own chunk (kind=query — doc2query, so a
// victim's own phrasing has a matching vector), embed everything, and store
// the document with all its chunks atomically.
//
// The contextual prefix ("<title> (loại: <scam_type>)") is added to the
// EMBEDDING INPUT only — the stored chunk content (and therefore the
// generated content_tsv) stays unprefixed, so the lexical arm's term
// statistics aren't diluted by a title repeated on every chunk.
//
// Embedding happens before any DB write, so a provider failure leaves
// nothing behind. Empty/whitespace-only content is rejected up front.
func (ix *Indexer) IndexDocument(ctx context.Context, doc corpusdoc.Document) (Result, error) {
	content := strings.TrimSpace(doc.Content)
	if content == "" {
		return Result{}, fmt.Errorf("ingest: %q has no indexable content", doc.URL)
	}

	stored := make([]string, 0, 1+len(doc.UserQueries))
	kinds := make([]model.ChunkKind, 0, cap(stored))
	embedInput := make([]string, 0, cap(stored))
	prefix := contextualPrefix(doc.Title, doc.ScamType)

	stored = append(stored, content)
	kinds = append(kinds, model.ChunkKindContent)
	embedInput = append(embedInput, prefix+content)

	for _, q := range doc.UserQueries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		stored = append(stored, q)
		kinds = append(kinds, model.ChunkKindQuery)
		embedInput = append(embedInput, prefix+q)
	}

	vectors, err := ix.embedChunks(ctx, embedInput)
	if err != nil {
		return Result{}, err
	}

	keep := dropNearDuplicateQueries(kinds, vectors, nearDupCosineThreshold)
	chunks := make([]model.Chunk, len(keep))
	for i, idx := range keep {
		chunks[i] = model.Chunk{Kind: kinds[idx], Content: stored[idx], Embedding: vectors[idx]}
	}

	docID, err := ix.store.InsertDocumentWithChunks(ctx, model.Document{
		URL:        doc.URL,
		Title:      doc.Title,
		Content:    doc.Content,
		Prevention: doc.Prevention,
		ScamType:   doc.ScamType,
		Source:     sourceCorpus,
	}, chunks)
	if err != nil {
		return Result{}, err
	}
	return Result{DocID: docID, Chunks: len(chunks)}, nil
}

// contextualPrefix is prepended to the embedding input of every chunk of a
// document (never to the stored/tsvector text — see IndexDocument's doc
// comment). This is the cheap Contextual Retrieval variant: it gives the
// dense arm a doc-identity signal without an LLM call per chunk.
func contextualPrefix(title, scamType string) string {
	return fmt.Sprintf("%s (loại: %s)\n", title, scamType)
}

// dropNearDuplicateQueries returns the indices (into kinds/vectors) to keep:
// every content chunk, plus every query chunk that is not a near-duplicate
// (cosine similarity > threshold) of an already-kept query chunk of the same
// document. Order is preserved.
func dropNearDuplicateQueries(kinds []model.ChunkKind, vectors [][]float32, threshold float64) []int {
	var kept []int
	var keptQueryVecs [][]float32
	for i, k := range kinds {
		if k != model.ChunkKindQuery {
			kept = append(kept, i)
			continue
		}
		dup := false
		for _, kv := range keptQueryVecs {
			if cosineSimilarity(vectors[i], kv) > threshold {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, i)
			keptQueryVecs = append(keptQueryVecs, vectors[i])
		}
	}
	return kept
}

// cosineSimilarity returns the cosine similarity of a and b, in [-1, 1].
// Both are assumed the same length — they always come from the same
// embedder call here.
func cosineSimilarity(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
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
