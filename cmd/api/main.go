// Command api serves the ChậmLại.vn scam-scoring HTTP API. It wires the RAG
// pipeline (config → store → embedder → retriever → llm → analyzer) via
// constructor injection and exposes POST /v1/analyze and GET /health. The
// HTTP handlers live in internal/api; the same wiring is smoke-tested by
// cmd/seed.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	setupLogger(cfg)

	if cfg.VoyageAPIKey == "" {
		slog.Error("VOYAGE_API_KEY is required")
		os.Exit(1)
	}
	if cfg.AnthropicAPIKey == "" {
		slog.Error("ANTHROPIC_API_KEY is required")
		os.Exit(1)
	}

	// Cancelled on SIGINT/SIGTERM; api.Run uses this to start a graceful
	// shutdown that drains in-flight requests before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("store", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		slog.Error("embedder", "error", err)
		os.Exit(1)
	}
	slog.Info("embedder ready", "model", emb.Model(), "dims", emb.Dimensions())

	llmSvc, err := llm.New(cfg.LLM())
	if err != nil {
		slog.Error("llm", "error", err)
		os.Exit(1)
	}
	slog.Info("llm ready", "model", llmSvc.Model())

	ret := retriever.New(emb, st)
	scorer := analyzer.New(llmSvc)
	analyzeHandler := analyze.New(ret, scorer)

	apiCfg := cfg.API()
	router := api.NewRouter(apiCfg, analyzeHandler)
	srv := api.NewServer(apiCfg, router)

	slog.Info("API listening", "addr", apiCfg.Addr)
	if err := api.Run(ctx, srv); err != nil {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
	slog.Info("API stopped")
}

// setupLogger installs the process-wide slog default: human-readable text in
// development, structured JSON otherwise (log aggregators expect JSON).
func setupLogger(cfg config.Configuration) {
	var handler slog.Handler
	if cfg.IsDevelopment() {
		handler = slog.NewTextHandler(os.Stdout, nil)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(handler))
}
