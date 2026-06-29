// Command crawler builds the scam-warning corpus: it reads a seed list of
// article urls plus any hand-curated local files, fetches and parses each one,
// labels it with a rule-based scam type, and runs it through the same
// ingest.IndexDocument pipeline that cmd/seed and the API query side use — so
// what we index stays consistent with what we later search.
//
// It is meant to be run by hand, repeatedly. Re-running is safe: a url already
// in the corpus is skipped before any embedding call, so a second run costs
// (almost) nothing. Per-url failures are logged and skipped; the batch always
// finishes and prints a summary.
//
//	docker compose up -d db
//	goose -dir migrations postgres "$DATABASE_URL" up   # needs 0003 (UNIQUE url)
//	VOYAGE_API_KEY=... go run ./cmd/crawler
//
// Seed urls live in cmd/crawler/data/ (git-ignored). One url per line; blank
// lines and '#' comments are skipped. Local files are *.md with a small
// "---"-fenced frontmatter (title, scam_type, source, url) — the path for
// content that can't be crawled, e.g. a YouTube transcript exported by hand.
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"sync"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/crawler"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/ingest"
)

func main() {
	seedsPath := flag.String("seeds", "cmd/crawler/data/seeds_20250625.txt", "seed file: one article url per line")
	filesGlob := flag.String("files", "cmd/crawler/data/*.md", "glob for hand-curated local documents")
	concurrency := flag.Int("concurrency", 5, "max concurrent fetch+index workers")
	flag.Parse()

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.VoyageAPIKey == "" {
		log.Fatal("VOYAGE_API_KEY is required")
	}

	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	log.Printf("embedder ready: model=%s dims=%d", emb.Model(), emb.Dimensions())

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	ix := ingest.New(emb, st)
	cr := crawler.New()

	// Collect work items up front so a bad seed/glob fails before we start
	// spending on embeddings.
	urls, err := crawler.LoadURLs(*seedsPath)
	if err != nil {
		log.Printf("no seed urls: %v", err)
	}
	files, err := filepath.Glob(*filesGlob)
	if err != nil {
		log.Fatalf("files glob %q: %v", *filesGlob, err)
	}
	log.Printf("queued %d url(s) and %d local file(s); concurrency=%d", len(urls), len(files), *concurrency)

	w := &worker{ctx: ctx, st: st, ix: ix, cr: cr}
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup

	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.handleURL(u)
		}(u)
	}
	for _, f := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			w.handleFile(f)
		}(f)
	}
	wg.Wait()

	n, s, e := w.totals()
	log.Printf("done: %d new, %d skipped, %d errors", n, s, e)
}

// worker carries the shared collaborators and a concurrency-safe result tally.
type worker struct {
	ctx context.Context
	st  *store.Store
	ix  *ingest.Indexer
	cr  *crawler.Crawler

	mu                sync.Mutex
	nNew, nSkip, nErr int
}

func (w *worker) totals() (int, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nNew, w.nSkip, w.nErr
}

func (w *worker) skip(format string, args ...any) {
	w.mu.Lock()
	w.nSkip++
	w.mu.Unlock()
	log.Printf(format, args...)
}
func (w *worker) fail(format string, args ...any) {
	w.mu.Lock()
	w.nErr++
	w.mu.Unlock()
	log.Printf(format, args...)
}
func (w *worker) ok(format string, args ...any) {
	w.mu.Lock()
	w.nNew++
	w.mu.Unlock()
	log.Printf(format, args...)
}

// handleURL crawls one seed url. It checks the corpus before fetching so a
// re-run skips a known url without a network round-trip or an embedding call.
func (w *worker) handleURL(url string) {
	if w.exists(url) {
		return
	}
	doc, err := w.cr.Fetch(w.ctx, url)
	if err != nil {
		w.fail("skip %s: %v", url, err)
		return
	}
	w.index(doc, crawler.InferScamType(doc.Title, doc.Content))
}

// handleFile ingests one hand-curated local file. Parsing is cheap, so it
// happens before the corpus check; the frontmatter scam_type wins over the
// inferred one when present.
func (w *worker) handleFile(path string) {
	doc, scamType, err := crawler.ParseLocalFile(path)
	if err != nil {
		w.fail("skip %s: %v", path, err)
		return
	}
	if w.exists(doc.URL) {
		return
	}
	if scamType == "" {
		scamType = crawler.InferScamType(doc.Title, doc.Content)
	}
	w.index(doc, scamType)
}

// exists reports whether url is already in the corpus, logging a skip if so. A
// lookup error is treated as "not present" so the indexing attempt proceeds and
// the UNIQUE(url) constraint stays the final guard.
func (w *worker) exists(url string) bool {
	present, err := w.st.DocumentExists(w.ctx, url)
	if err != nil {
		log.Printf("warn: exists check %s: %v", url, err)
		return false
	}
	if present {
		w.skip("skip (exists): %s", url)
	}
	return present
}

// index runs the embed+store pipeline and tallies the outcome. A unique
// violation means another worker indexed the same url between our check and our
// insert (or a duplicate sits in the seed list) — counted as a skip, not an
// error, so the embedding cost is the only loss.
func (w *worker) index(doc crawler.FetchedDoc, scamType string) {
	_, err := w.ix.IndexDocument(w.ctx, ingest.Document{
		URL:      doc.URL,
		Title:    doc.Title,
		Content:  doc.Content,
		ScamType: scamType,
		Source:   doc.Source,
	})
	switch {
	case store.IsUniqueViolation(err):
		w.skip("skip (race dup): %s", doc.URL)
	case err != nil:
		w.fail("skip %s: %v", doc.URL, err)
	default:
		w.ok("indexed [%s] %s (%s)", scamType, doc.URL, doc.Source)
	}
}
