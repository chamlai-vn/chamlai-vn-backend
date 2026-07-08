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

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
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
