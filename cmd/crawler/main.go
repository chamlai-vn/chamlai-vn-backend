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
//	-mode=audit     lists cross-document near-duplicate chunk pairs (same kind,
//	                same scam_type, cosine similarity >= -threshold) for review.
//	                Read-only; needs a database only — no embedder, LLM, or crawler.
//	-mode=prune     deletes an explicit -chunks=<ids> list (picked from an audit
//	                report). Dry-run unless -apply. Needs a database only.
//
// The modes branch before constructing any collaborator, so each only
// requires the secrets it actually uses. audit/prune read stored vectors, so
// unlike ingest they need no VOYAGE_API_KEY.
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

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
)

func main() {
	mode := flag.String("mode", "", `pipeline stage: "generate" (fetch+enrich -> data/corpus/*.md for review) or "ingest" (reviewed data/corpus/*.md -> embed+store)`)
	seedsPath := flag.String("seeds", "data/seeds/seeds_20250625.txt", "generate: seed file, one article url per line")
	outDir := flag.String("out", "data/corpus", "generate: output directory for generated .md files")
	corpusGlob := flag.String("corpus", "data/corpus/*.md", "ingest: glob for reviewed corpus markdown files")
	concurrency := flag.Int("concurrency", 5, "max concurrent workers")
	scamType := flag.String("scam-type", "", "audit: restrict to one scam_type (empty = all)")
	threshold := flag.Float64("threshold", 0.95, "audit: min cosine similarity to flag a duplicate pair")
	previewLen := flag.Int("preview", 120, "audit: content preview length in characters")
	chunkIDs := flag.String("chunks", "", "prune: comma-separated chunk ids to delete")
	apply := flag.Bool("apply", false, "prune: actually delete (default is dry-run)")
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
	case "audit":
		if *threshold <= 0 || *threshold > 1 {
			log.Fatalf("crawler: -threshold must be in (0,1], got %v", *threshold)
		}
		runAudit(ctx, cfg, *threshold, *scamType, *previewLen)
	case "prune":
		ids, err := parseChunkIDs(*chunkIDs)
		if err != nil {
			log.Fatalf("crawler: -chunks: %v", err)
		}
		if len(ids) == 0 {
			log.Fatal("crawler: -chunks must list at least one numeric chunk id")
		}
		runPrune(ctx, cfg, ids, *apply)
	default:
		log.Fatalf(`crawler: -mode must be "generate", "ingest", "audit", or "prune", got %q`, *mode)
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
