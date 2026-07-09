// Command benchmark measures the end-to-end value of the RAG scam-scoring
// pipeline against a generic AI baseline (Sonnet + web search), on a
// generated dataset of Vietnamese scam and benign messages. This is a
// different axis from benchmark/README.md's retrieval-quality harness
// (Recall@K/MRR, vector-vs-hybrid) — that one is deferred pending a bigger
// corpus; this one answers "is the product actually better than asking a
// generic AI", which doesn't depend on that question being resolved.
//
// Four phases, run separately or chained with -all. -run and -judge call
// paid, slow LLM/web-search APIs and checkpoint per test case (one file per
// case under the run directory) so a crash or Ctrl-C loses at most the
// in-flight case, and re-running skips whatever's already on disk:
//
//	-gen     Haiku generates a stratified dataset (120 scam + 40 benign,
//	         ground truth assigned by the harness, not guessed by the model)
//	         and writes it to dataset.json in a new run directory.
//	-run     Scores every case with both arms — rag-hybrid (mirrors
//	         /v1/analyze) and generic-websearch (Sonnet + web search) — and
//	         checkpoints each (case, arm) pair to arms/<case_id>.<arm>.json.
//	-judge   Opus scores both arms' answers per case, dual-order (two calls,
//	         swapped which arm is "A"), and checkpoints to judged/<id>.json.
//	-report  Reads arms+judged, computes confusion matrices via
//	         pkg/util/eval, and writes results.json + summary.csv +
//	         report.html into the run directory.
//
// Usage:
//
//	# One shot — needs VOYAGE_API_KEY + ANTHROPIC_API_KEY (+ GEMINI_API_KEY for
//	# the optional cross-family judge validation subset).
//	VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... go run ./cmd/benchmark -all
//
//	# Or phase by phase, e.g. to review the dataset before spending money:
//	ANTHROPIC_API_KEY=... go run ./cmd/benchmark -gen
//	# review benchmark/results/<dir>/dataset.json ...
//	VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... go run ./cmd/benchmark -run -dir <dir>
//	ANTHROPIC_API_KEY=... go run ./cmd/benchmark -judge -dir <dir>
//	go run ./cmd/benchmark -report -dir <dir>
//
//	# Cheap smoke test of the whole pipeline before burning real money:
//	go run ./cmd/benchmark -all -limit 5
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
)

func main() {
	gen := flag.Bool("gen", false, "generate a new stratified dataset into a new run directory")
	run := flag.Bool("run", false, "score every dataset case with both arms")
	judge := flag.Bool("judge", false, "score both arms' answers per case with an LLM judge")
	report := flag.Bool("report", false, "write results.json + summary.csv + report.html")
	all := flag.Bool("all", false, "run gen, run, judge, report in sequence in one new run directory")

	dir := flag.String("dir", "", "run directory name under -results (required for -run/-judge/-report standalone; auto-generated for -gen/-all)")
	resultsRoot := flag.String("results", "benchmark/results", "root directory all run directories live under")
	limit := flag.Int("limit", 0, "cap the number of dataset cases processed this invocation (0 = no cap); applies to every phase")

	concurrencyRAG := flag.Int("concurrency-rag", 5, "-run: max concurrent rag-hybrid arm workers")
	concurrencyGeneric := flag.Int("concurrency-generic", 4, "-run: max concurrent generic-websearch arm workers (heavier per-call than rag)")
	maxUses := flag.Int("max-uses", 4, "-run: max web_search tool uses per generic-arm case")
	crossFamilyN := flag.Int("cross-family-n", 30, "-judge: number of cases to additionally score with a cross-family (Gemini) judge for self-preference-bias validation; 0 disables")
	flag.Parse()

	if !*gen && !*run && !*judge && !*report && !*all {
		log.Fatal("benchmark: at least one of -gen, -run, -judge, -report, -all is required")
	}

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if *all {
		runDir := newRunDir(*resultsRoot)
		log.Printf("run directory: %s", runDir)
		runGen(ctx, cfg, runDir, *limit)
		runRun(ctx, cfg, runDir, *limit, *concurrencyRAG, *concurrencyGeneric, *maxUses)
		runJudge(ctx, cfg, runDir, *limit, *crossFamilyN)
		runReport(runDir)
		return
	}

	// Standalone phases: -gen mints a fresh run directory unless -dir names
	// an existing one to regenerate into; every other phase requires -dir.
	runDir := *dir
	if runDir == "" {
		if !*gen {
			log.Fatal("benchmark: -dir is required for -run/-judge/-report (name the run directory a prior -gen created)")
		}
		runDir = newRunDir(*resultsRoot)
	} else {
		runDir = filepath.Join(*resultsRoot, runDir)
	}
	log.Printf("run directory: %s", runDir)

	if *gen {
		runGen(ctx, cfg, runDir, *limit)
	}
	if *run {
		runRun(ctx, cfg, runDir, *limit, *concurrencyRAG, *concurrencyGeneric, *maxUses)
	}
	if *judge {
		runJudge(ctx, cfg, runDir, *limit, *crossFamilyN)
	}
	if *report {
		runReport(runDir)
	}
}

// newRunDir mints a new, sortable, filesystem-safe run directory under root
// and creates it. RFC3339 has colons, which are awkward in filenames on some
// filesystems — use a compact variant instead.
func newRunDir(root string) string {
	name := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("benchmark: create run directory %q: %v", dir, err)
	}
	return dir
}
