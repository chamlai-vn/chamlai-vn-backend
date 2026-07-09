package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/reranker"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/v1/analyze"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

const armsDirName = "arms"

// genericModel is pinned independently of cfg.AnthropicModel: the generic
// arm must always be "a generic AI a real user would reach for today", not
// whatever model the rag-hybrid arm under test happens to be configured to.
const genericModel = "claude-sonnet-4-6"

// runRun scores every dataset case with both arms and checkpoints each
// (case, arm) pair to its own file under runDir/arms/ (see armFilePath) —
// concurrent workers across the two arm pools never contend for the same
// path, and a prior partial run resumes for free via skip-if-exists.
func runRun(ctx context.Context, cfg config.Configuration, runDir string, limit, concurrencyRAG, concurrencyGeneric, maxUses int) {
	if cfg.VoyageAPIKey == "" {
		log.Fatal("VOYAGE_API_KEY is required for -run")
	}
	if cfg.AnthropicAPIKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required for -run")
	}

	cases, err := readDataset(runDir)
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	if limit > 0 && limit < len(cases) {
		cases = cases[:limit]
	}
	log.Printf("run: %d case(s) queued", len(cases))

	if err := os.MkdirAll(filepath.Join(runDir, armsDirName), 0o755); err != nil {
		log.Fatalf("run: %v", err)
	}

	// --- RAG arm: build the exact same objects cmd/api/main.go builds, with
	// the exact same options, so this arm measures production fidelity
	// rather than a re-derived approximation of it. ---
	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	log.Printf("embedder ready: model=%s dims=%d", emb.Model(), emb.Dimensions())
	if emb.Dimensions() != store.EmbeddingDimensions {
		log.Fatalf("embedder dimensions = %d, want %d", emb.Dimensions(), store.EmbeddingDimensions)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	rr, err := reranker.New(cfg.Reranker())
	if err != nil {
		log.Fatalf("reranker: %v", err)
	}

	ragLLM, err := llm.New(cfg.LLM())
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	log.Printf("rag arm ready: model=%s", ragLLM.Model())

	ret := retriever.New(emb, st, retriever.WithReranker(rr)) // == cmd/api/main.go:86
	scorer := analyzer.New(ragLLM)                            // == cmd/api/main.go:87

	// --- Generic arm: raw web-search client for the assess phase (see
	// websearch.go — this can't go through llm.Service, which only supports
	// forced tool-use) plus a plain Anthropic Service for the structuring
	// phase, both pinned to genericModel. ---
	ws := newAnthropicWebSearcher(cfg.AnthropicAPIKey, genericModel, maxUses)
	structureLLM := llm.NewAnthropic(llm.AnthropicConfig{APIKey: cfg.AnthropicAPIKey, Model: genericModel})

	var wg sync.WaitGroup
	var ragTally, genericTally tally

	wg.Add(1)
	go func() {
		defer wg.Done()
		runArmPool(ctx, runDir, cases, ArmRAGHybrid, concurrencyRAG, &ragTally, func(ctx context.Context, tc TestCase) (ArmOutput, error) {
			return scoreRAG(ctx, ret, scorer, tc, analyze.DefaultTopK)
		})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		runArmPool(ctx, runDir, cases, ArmGenericWebSearch, concurrencyGeneric, &genericTally, func(ctx context.Context, tc TestCase) (ArmOutput, error) {
			return scoreGeneric(ctx, ws, structureLLM, tc)
		})
	}()
	wg.Wait()

	ragNew, ragSkip, ragErr := ragTally.totals()
	genNew, genSkip, genErr := genericTally.totals()
	log.Printf("run done: rag-hybrid %d scored, %d skipped, %d errors", ragNew, ragSkip, ragErr)
	log.Printf("run done: generic-websearch %d scored, %d skipped, %d errors", genNew, genSkip, genErr)

	meta := readMeta(runDir)
	meta.RAGModel = ragLLM.Model()
	meta.RAGTopK = analyze.DefaultTopK
	meta.RerankerEnabled = true
	meta.GenericModel = genericModel
	meta.WebSearchMaxUses = maxUses
	if err := writeMeta(runDir, meta); err != nil {
		log.Fatalf("run: %v", err)
	}
}

// runArmPool runs score over every case in cases with concurrency workers,
// skipping cases whose checkpoint file already exists and writing a
// checkpoint only on success — a hard failure is logged (via t) and left for
// the next -run invocation to retry, never checkpointed.
func runArmPool(ctx context.Context, runDir string, cases []TestCase, arm string, concurrency int, t *tally, score func(context.Context, TestCase) (ArmOutput, error)) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, tc := range cases {
		path := armFilePath(runDir, tc.ID, arm)
		if _, err := os.Stat(path); err == nil {
			t.skip("skip (exists): %s [%s]", tc.ID, arm)
			continue
		}

		wg.Add(1)
		go func(tc TestCase, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			out, err := score(ctx, tc)
			if err != nil {
				t.fail("error %s [%s]: %v", tc.ID, arm, err)
				return
			}
			out.CaseID = tc.ID
			out.Arm = arm
			out.LatencyMS = time.Since(start).Milliseconds()

			if err := writeArmOutput(path, out); err != nil {
				t.fail("write %s [%s]: %v", tc.ID, arm, err)
				return
			}
			t.ok("scored %s [%s]", tc.ID, arm)
		}(tc, path)
	}
	wg.Wait()
}

// scoreRAG runs the production retrieve-then-score pipeline directly
// (bypassing HTTP), mirroring internal/api/v1/analyze/analyze.go's Handle.
func scoreRAG(ctx context.Context, ret *retriever.Retriever, scorer analyzer.Scorer, tc TestCase, topK int) (ArmOutput, error) {
	chunks, err := ret.HybridSearch(ctx, tc.Text, topK)
	if err != nil {
		return ArmOutput{}, fmt.Errorf("retrieve: %w", err)
	}
	result, err := scorer.Score(ctx, tc.Text, chunks)
	if err != nil {
		return ArmOutput{}, fmt.Errorf("score: %w", err)
	}
	return ArmOutput{Result: *result}, nil
}

// armFilePath is the checkpoint path for one (case, arm) pair. One file per
// pair — never a shared file multiple workers append to — so concurrent
// writes can't interleave and corrupt JSON.
func armFilePath(runDir, caseID, arm string) string {
	return filepath.Join(runDir, armsDirName, fmt.Sprintf("%s.%s.json", caseID, arm))
}

func writeArmOutput(path string, out ArmOutput) error {
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, raw, 0o644)
}

func readArmOutput(path string) (ArmOutput, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ArmOutput{}, err
	}
	var out ArmOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return ArmOutput{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return out, nil
}
