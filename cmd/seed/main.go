// Command seed is a manual end-to-end smoke test for the RAG retrieval path:
// embed a sample scam-warning article, store it, then embed a suspicious query
// and print the nearest chunks. Run it after applying migrations:
//
//	docker compose up -d db
//	goose -dir migrations postgres "$DATABASE_URL" up
//	VOYAGE_API_KEY=... go run ./cmd/seed
//
// It inserts a fresh document each run (no dedup) — fine for a smoke test.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chamlai-vn/chamlai-vn-backend/config"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/embedder"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/ingest"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/store"
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
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.VoyageAPIKey == "" {
		log.Fatal("VOYAGE_API_KEY is required")
	}
	dsn := cfg.DatabaseURL

	emb, err := embedder.New(cfg.Embedder())
	if err != nil {
		log.Fatalf("embedder: %v", err)
	}
	log.Printf("embedder ready: model=%s dims=%d", emb.Model(), emb.Dimensions())

	st, err := store.New(ctx, dsn)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	// 1. Run the full indexing pipeline: chunk → embed each chunk → store.
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

	// 2. Embed the suspicious message as a query and retrieve nearest chunks.
	qVecs, err := emb.Embed(ctx, []string{suspiciousQuery}, embedder.InputQuery)
	if err != nil {
		log.Fatalf("embed query: %v", err)
	}
	matches, err := st.SearchSimilar(ctx, qVecs[0], 5)
	if err != nil {
		log.Fatalf("search: %v", err)
	}

	fmt.Printf("\nQuery: %s\n\nTop %d matches:\n", suspiciousQuery, len(matches))
	for i, m := range matches {
		fmt.Printf("  %d. distance=%.4f scam_type=%s chunk=%d\n     %s\n",
			i+1, m.Distance, m.ScamType, m.ChunkID, snippet(m.Content, 80))
	}
}

func snippet(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
