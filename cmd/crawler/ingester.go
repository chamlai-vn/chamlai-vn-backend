package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/ingest"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

// runIngest wires a database+embedder (no LLM, no crawler) and indexes every
// reviewed file matching corpusGlob.
func runIngest(ctx context.Context, cfg config.Configuration, corpusGlob string, concurrency int) {
	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	log.Printf("embedder ready: model=%s dims=%d", emb.Model(), emb.Dimensions())
	if emb.Dimensions() != store.EmbeddingDimensions {
		log.Fatalf("embedder dimensions = %d, want %d (chunks.embedding column)",
			emb.Dimensions(), store.EmbeddingDimensions)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	files, err := filepath.Glob(corpusGlob)
	if err != nil {
		log.Fatalf("corpus glob %q: %v", corpusGlob, err)
	}
	log.Printf("ingest: %d file(s) queued; glob=%s; concurrency=%d", len(files), corpusGlob, concurrency)

	it := &ingester{ctx: ctx, st: st, ix: ingest.New(emb, st)}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			it.handleFile(f)
		}(f)
	}
	wg.Wait()

	n, s, e := it.totals()
	log.Printf("ingest done: %d indexed, %d skipped, %d errors", n, s, e)
}

// ingester parses+indexes one reviewed corpus file per handleFile call.
type ingester struct {
	ctx context.Context
	st  *store.Store
	ix  *ingest.Indexer
	tally
}

// handleFile enforces the review gate (refuses "reviewed: false" — the
// technical enforcement behind "a human reviews every generated file", not
// just a documentation convention), then parses and indexes the file. A
// unique violation means the url was already indexed by an earlier/
// concurrent run — counted as a skip, not an error.
func (it *ingester) handleFile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		it.fail("skip %s: read: %v", path, err)
		return
	}

	reviewed, err := corpusdoc.ReadReviewedFlag(string(raw))
	if err != nil {
		it.fail("skip %s: %v", path, err)
		return
	}
	if !reviewed {
		it.skip("skip (reviewed: false): %s", path)
		return
	}

	doc, err := corpusdoc.Parse(string(raw))
	if err != nil {
		it.fail("skip %s: %v", path, err)
		return
	}

	if exists, err := it.st.DocumentExists(it.ctx, doc.URL); err != nil {
		log.Printf("warn: exists check %s: %v", doc.URL, err)
	} else if exists {
		it.skip("skip (exists): %s", doc.URL)
		return
	}

	res, err := it.ix.IndexDocument(it.ctx, doc)
	switch {
	case store.IsUniqueViolation(err):
		it.skip("skip (race dup): %s", doc.URL)
	case err != nil:
		it.fail("skip %s: %v", path, err)
	default:
		it.ok("indexed [%s] %s (%d chunks)", doc.ScamType, doc.URL, res.Chunks)
	}
}
