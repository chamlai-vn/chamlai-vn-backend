// Command api serves the ChậmLại.vn scam-scoring HTTP API. It wires the RAG
// pipeline (config → store → embedder → retriever → llm → analyzer) via
// constructor injection and exposes POST /analyze and GET /health. The HTTP
// handlers live in internal/api; the same wiring is smoke-tested by cmd/seed.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/v1/analyze"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.VoyageAPIKey == "" {
		log.Fatal("VOYAGE_API_KEY is required")
	}
	if cfg.AnthropicAPIKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	log.Printf("embedder ready: model=%s dims=%d", emb.Model(), emb.Dimensions())

	llmSvc, err := llm.New(cfg.LLM())
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	log.Printf("analyzer ready: model=%s", llmSvc.Model())

	ret := retriever.New(emb, st)
	scorer := analyzer.New(llmSvc)
	h := analyze.New(ret, scorer)

	routerCfg := api.Config{AllowOrigins: []string{"*"}, BodyLimitBytes: 64 * 1024}
	addr := ":" + cfg.Port
	log.Printf("API listening on %s", addr)
	if err := http.ListenAndServe(addr, api.NewRouter(routerCfg, h)); err != nil {
		log.Fatalf("server: %v", err)
	}
}
