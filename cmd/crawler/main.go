// Command crawler builds the scam-warning corpus in two reviewable stages:
//
//	-mode=generate  fetches seed urls, runs them through an LLM (internal/scam/enrich)
//	                to produce the canonical 4-section corpusdoc format, and writes
//	                each one to data/corpus/<slug>.md marked "reviewed: false". It
//	                needs crawler+enrich+an LLM key — no database, no embedder.
//	-mode=ingest    reads data/corpus/*.md, refuses any file still marked
//	                "reviewed: false", and runs the rest through the same
//	                ingest.IndexDocument pipeline cmd/seed and the API query side
//	                use — so what we index stays consistent with what we later
//	                search. It needs a database + embedder — no LLM, no crawler.
//
// The two modes branch before constructing any collaborator, so each only
// requires the secrets it actually uses.
//
// A human reviews/edits every file generate# writes — flipping "reviewed:
// false" to "reviewed: true" — before running -mode=ingest. This is the only
// gate between LLM output and the stored corpus.
//
//	docker compose up -d db
//	go run ./cmd/migration up
//	ANTHROPIC_API_KEY=... go run ./cmd/crawler -mode=generate
//	# review/edit data/corpus/*.md, flip reviewed: true
//	VOYAGE_API_KEY=... go run ./cmd/crawler -mode=ingest
//
// Both modes are safe to re-run: generate skips a url that already has a
// generated file; ingest skips a url already in the corpus before any
// embedding call.
//
// Seed urls for -mode=generate live in data/seeds/ (git-ignored except a
// synthetic example). One url per line; blank lines and '#' comments are
// skipped.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/enrich"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/ingest"
	"github.com/chamlai-vn/chamlai-vn-backend/pkg/util/corpusdoc"
)

func main() {
	mode := flag.String("mode", "", `pipeline stage: "generate" (fetch+enrich -> data/corpus/*.md for review) or "ingest" (reviewed data/corpus/*.md -> embed+store)`)
	seedsPath := flag.String("seeds", "data/seeds/seeds_20250625.txt", "generate: seed file, one article url per line")
	outDir := flag.String("out", "data/corpus", "generate: output directory for generated .md files")
	corpusGlob := flag.String("corpus", "data/corpus/*.md", "ingest: glob for reviewed corpus markdown files")
	concurrency := flag.Int("concurrency", 5, "max concurrent workers")
	flag.Parse()

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Branch BEFORE constructing any collaborator: generate needs
	// crawler+enrich+an LLM key only; ingest needs a database+embedder only.
	// Neither mode should be blocked on a secret it doesn't use.
	switch *mode {
	case "generate":
		requireLLMKey(cfg)
		runGenerate(ctx, cfg, *seedsPath, *outDir, *concurrency)
	case "ingest":
		if cfg.VoyageAPIKey == "" {
			log.Fatal("VOYAGE_API_KEY is required for -mode=ingest")
		}
		runIngest(ctx, cfg, *corpusGlob, *concurrency)
	default:
		log.Fatalf(`crawler: -mode must be "generate" or "ingest", got %q`, *mode)
	}
}

// requireLLMKey fails fast (matching cmd/api/cmd/seed's startup-check
// pattern) if the key for the configured LLM_PROVIDER is missing, rather
// than letting -mode=generate run the whole crawl batch before failing on
// the first API call.
func requireLLMKey(cfg config.Configuration) {
	switch llm.Provider(cfg.LLMProvider) {
	case llm.ProviderAnthropic:
		if cfg.AnthropicAPIKey == "" {
			log.Fatal("ANTHROPIC_API_KEY is required for -mode=generate (LLM_PROVIDER=anthropic)")
		}
	case llm.ProviderGemini:
		if cfg.GeminiAPIKey == "" {
			log.Fatal("GEMINI_API_KEY is required for -mode=generate (LLM_PROVIDER=gemini)")
		}
	case llm.ProviderOpenAI:
		if cfg.OpenAIAPIKey == "" {
			log.Fatal("OPENAI_API_KEY is required for -mode=generate (LLM_PROVIDER=openai)")
		}
	default:
		log.Fatalf("crawler: unknown LLM_PROVIDER %q", cfg.LLMProvider)
	}
}

// tally is a concurrency-safe new/skip/error counter shared by both modes'
// worker pools.
type tally struct {
	mu                sync.Mutex
	nNew, nSkip, nErr int
}

func (t *tally) totals() (int, int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.nNew, t.nSkip, t.nErr
}
func (t *tally) skip(format string, args ...any) {
	t.mu.Lock()
	t.nSkip++
	t.mu.Unlock()
	log.Printf(format, args...)
}
func (t *tally) fail(format string, args ...any) {
	t.mu.Lock()
	t.nErr++
	t.mu.Unlock()
	log.Printf(format, args...)
}
func (t *tally) ok(format string, args ...any) {
	t.mu.Lock()
	t.nNew++
	t.mu.Unlock()
	log.Printf(format, args...)
}

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
