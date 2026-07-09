package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/enrich"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

// runGenerate wires crawler+enrich+llm (no DB, no embedder) and fetches each
// seed url, enriches it, and writes the result to outDir marked unreviewed.
func runGenerate(ctx context.Context, cfg config.Configuration, seedsPath, outDir string, concurrency int) {
	llmSvc, err := llm.New(cfg.LLM())
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	log.Printf("llm ready: model=%s", llmSvc.Model())

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir %q: %v", outDir, err)
	}

	urls, err := crawler.LoadURLs(seedsPath)
	if err != nil {
		log.Fatalf("load seeds %q: %v", seedsPath, err)
	}
	log.Printf("generate: %d url(s) queued; concurrency=%d; out=%s", len(urls), concurrency, outDir)

	g := &generator{ctx: ctx, cr: crawler.New(), en: enrich.New(llmSvc), outDir: outDir}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			g.handleURL(u)
		}(u)
	}
	wg.Wait()

	n, s, e := g.totals()
	log.Printf("generate done: %d written, %d skipped, %d errors", n, s, e)
}

// generator fetches+enriches one url per handleURL call and writes the
// result as an unreviewed corpus markdown file.
type generator struct {
	ctx    context.Context
	cr     *crawler.Crawler
	en     *enrich.Enricher
	outDir string
	tally
}

// handleURL fetches rawURL, enriches it into a corpusdoc.Document, and
// writes it to outDir. The skip-if-exists check is file-based (generate has
// no database): it computes the target filename from the url alone —
// corpusdoc.Slug's hash suffix depends only on URL, so this matches the
// filename the post-enrich Document would produce in the common case where
// the URL has a path segment (see corpusdoc.Slug's doc comment for the rare
// no-path fallback case, where a redundant re-generation is possible but
// harmless — -mode=ingest's UNIQUE(url) constraint is the real dedup guard).
func (g *generator) handleURL(rawURL string) {
	path := filepath.Join(g.outDir, corpusdoc.Slug(corpusdoc.Document{URL: rawURL})+".md")
	if _, err := os.Stat(path); err == nil {
		g.skip("skip (already generated): %s -> %s", rawURL, path)
		return
	}

	fetched, err := g.cr.Fetch(g.ctx, rawURL)
	if err != nil {
		g.fail("skip %s: %v", rawURL, err)
		return
	}

	doc, err := g.en.Enrich(g.ctx, enrich.Input{
		URL:               fetched.URL,
		Title:             fetched.Title,
		Content:           fetched.Content,
		SuggestedScamType: crawler.InferScamType(fetched.Title, fetched.Content),
	})
	if err != nil {
		g.fail("skip %s: enrich: %v", rawURL, err)
		return
	}

	if err := os.WriteFile(path, []byte(corpusdoc.SerializeForReview(doc)), 0o644); err != nil {
		g.fail("skip %s: write %q: %v", rawURL, path, err)
		return
	}
	g.ok("generated [%s] %s -> %s (review before ingest)", doc.ScamType, rawURL, path)
}
