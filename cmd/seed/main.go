// Command seed is a manual end-to-end smoke test for the RAG retrieval path:
// embed a sample scam-warning article, store it, then embed a suspicious query
// and print the nearest chunks. Run it after applying migrations:
//
//	docker compose up -d db
//	goose -dir migrations postgres "$DATABASE_URL" up
//	VOYAGE_API_KEY=... go run ./cmd/seed
//	VOYAGE_API_KEY=... go run ./cmd/seed -query-only          # skip insert, query existing corpus
//	VOYAGE_API_KEY=... go run ./cmd/seed -q "câu hỏi khác"   # custom query
//
// Add -score to also run the analyzer (LLM scam scoring) on the query and print
// the red/yellow/green verdict (requires ANTHROPIC_API_KEY):
//
//	VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... go run ./cmd/seed -query-only -score
//
// Add -compare to also run HybridSearch (vector + keyword via RRF) on the same
// query and print its top-5 next to the vector-only top-5, for comparing recall
// on keyword-heavy vs semantic-heavy queries. Needs a populated corpus (run
// cmd/crawler first) — the single sample document below isn't enough to see a
// meaningful difference:
//
//	VOYAGE_API_KEY=... go run ./cmd/seed -query-only -compare -q "bùa ngải vong hồn lừa đảo"
//
// Add -rerank to also print the fusion-only top-10 next to the fusion+rerank
// (Voyage rerank-2.5) top-5, so you can see whether reranking changed the
// order (on the current small corpus, it usually won't — that's expected):
//
//	VOYAGE_API_KEY=... go run ./cmd/seed -query-only -rerank -q "bùa ngải vong hồn lừa đảo"
//
// It inserts a fresh document each run unless -query-only is set.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/llm"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ai/reranker"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/analyzer"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/ingest"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/scam/retriever"
)

// A sample scam-warning article and a suspicious message that should retrieve it.
const (
	sampleScamType = "viec-nhe-luong-cao"
	sampleSource   = "seed"
	sampleURL      = "https://example.invalid/canh-bao-viec-nhe-luong-cao"
	sampleTitle    = "Cảnh báo: chiêu trò 'việc nhẹ lương cao' tuyển cộng tác viên online"
	sampleContent  = "Các đối tượng giả danh tuyển cộng tác viên chốt đơn online, " +
		"hứa hẹn việc nhẹ lương cao, hoa hồng hấp dẫn. Ban đầu trả tiền cho vài đơn " +
		"nhỏ để tạo lòng tin, sau đó dụ nạn nhân nạp số tiền lớn hơn rồi chiếm đoạt " +
		"và chặn liên lạc."

	suspiciousQuery = "Bên mình đang tuyển CTV làm việc tại nhà, chốt đơn nhận hoa hồng " +
		"ngay, hoàn vốn sau mỗi nhiệm vụ. Bạn nạp trước 500k để kích hoạt đơn nhé."
)

func main() {
	queryOnly := flag.Bool("query-only", false, "skip document insertion, query the existing corpus")
	customQuery := flag.String("q", "", "custom query string (overrides the default suspicious message)")
	score := flag.Bool("score", false, "run the analyzer on the query and print the red/yellow/green verdict (needs ANTHROPIC_API_KEY)")
	compare := flag.Bool("compare", false, "also run HybridSearch and print its top-5 next to the vector-only top-5")
	rerank := flag.Bool("rerank", false, "also rerank HybridSearch results (Voyage rerank-2.5) and print fusion-only top-10 next to fusion+rerank top-5")
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
	if emb.Dimensions() != store.EmbeddingDimensions {
		log.Fatalf("embedder dimensions = %d, want %d (chunks.embedding column)",
			emb.Dimensions(), store.EmbeddingDimensions)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	// 1. Index a sample document (skipped with -query-only).
	if !*queryOnly {
		indexer := ingest.New(emb, st)
		res, err := indexer.IndexDocument(ctx, ingest.Document{
			URL:      sampleURL,
			Title:    sampleTitle,
			Content:  sampleContent,
			ScamType: sampleScamType,
			Source:   sampleSource,
		})
		if err != nil {
			log.Fatalf("index document: %v", err)
		}
		log.Printf("stored document id=%d (%d chunks)", res.DocID, res.Chunks)
	}

	// 2. Retrieve the top-k nearest scam patterns via the retriever pipeline.
	query := suspiciousQuery
	if *customQuery != "" {
		query = *customQuery
	}
	ret := retriever.New(emb, st)
	results, err := ret.Retrieve(ctx, query, 5)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}

	fmt.Printf("\nQuery: %s\n\nTop %d matches (vector-only):\n", query, len(results))
	for i, r := range results {
		fmt.Printf("  %d. score=%.4f scam_type=%s url=%s\n     %s\n",
			i+1, r.Score, r.ScamType, r.SourceURL, snippet(r.Content, 80))
	}

	// 2b. Optionally run hybrid search on the same query for side-by-side comparison.
	if *compare {
		hybridResults, err := ret.HybridSearch(ctx, query, 5)
		if err != nil {
			log.Fatalf("hybrid search: %v", err)
		}
		fmt.Printf("\nTop %d matches (hybrid: vector + keyword via RRF):\n", len(hybridResults))
		for i, r := range hybridResults {
			fmt.Printf("  %d. rrf_score=%.4f scam_type=%s url=%s\n     %s\n",
				i+1, r.Score, r.ScamType, r.SourceURL, snippet(r.Content, 80))
		}
	}

	// 2c. Optionally rerank HybridSearch results and print fusion-only vs
	// fusion+rerank side by side.
	if *rerank {
		rr, err := reranker.New(cfg.Reranker())
		if err != nil {
			log.Fatalf("reranker: %v", err)
		}
		log.Printf("reranker ready: model=%s", rr.Model())

		fusionOnly, err := ret.HybridSearch(ctx, query, 10)
		if err != nil {
			log.Fatalf("hybrid search (fusion only): %v", err)
		}
		fmt.Printf("\nTop %d matches (hybrid fusion only, pre-rerank):\n", len(fusionOnly))
		for i, r := range fusionOnly {
			fmt.Printf("  %d. rrf_score=%.4f scam_type=%s url=%s\n     %s\n",
				i+1, r.Score, r.ScamType, r.SourceURL, snippet(r.Content, 80))
		}

		retRerank := retriever.New(emb, st, retriever.WithReranker(rr))
		reranked, err := retRerank.HybridSearch(ctx, query, 5)
		if err != nil {
			log.Fatalf("hybrid search (rerank): %v", err)
		}
		fmt.Printf("\nTop %d matches (hybrid + rerank-2.5):\n", len(reranked))
		for i, r := range reranked {
			fmt.Printf("  %d. rerank_score=%.4f scam_type=%s url=%s\n     %s\n",
				i+1, r.Score, r.ScamType, r.SourceURL, snippet(r.Content, 80))
		}
	}

	// 3. Optionally score the query against the retrieved patterns.
	if *score {
		if cfg.AnthropicAPIKey == "" {
			log.Fatal("ANTHROPIC_API_KEY is required for -score")
		}
		llmSvc, err := llm.New(cfg.LLM())
		if err != nil {
			log.Fatalf("llm: %v", err)
		}
		log.Printf("analyzer ready: model=%s", llmSvc.Model())

		verdict, err := analyzer.New(llmSvc).Score(ctx, query, results)
		if err != nil {
			log.Fatalf("score: %v", err)
		}
		out, _ := json.MarshalIndent(verdict, "", "  ")
		fmt.Printf("\nVerdict:\n%s\n", out)
	}
}

func snippet(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
