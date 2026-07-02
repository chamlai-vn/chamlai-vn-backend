# Retrieval Benchmark — thiết kế & trạng thái

> **Trạng thái: chuẩn bị nền, chưa chạy — một cách có chủ đích.**
> Tài liệu này ghi lại: benchmark so sánh các chiến lược retrieval khả thi đến đâu
> trên codebase hiện tại, thiết kế harness dự kiến, vì sao *chưa* chạy, và điều kiện
> nào thì quay lại chạy. (Origin: `docs/brainstorms/2026-07-02-retrieval-benchmark-requirements.md`)

## Câu hỏi cần trả lời

Hybrid search (vector + keyword qua RRF, PR #5) có thực sự cải thiện retrieval so với
pure vector similarity trên corpus cảnh báo lừa đảo tiếng Việt không? Rộng hơn: khi
các kỹ thuật RAG mới xuất hiện (contextual retrieval, đổi embedding model, tune tham
số RRF), lấy gì làm thước đo để quyết định adopt hay bỏ?

## Tính khả thi: codebase đã sẵn ~90%

Ba mảnh ghép cần cho một benchmark tự động đều đã tồn tại:

| Mảnh ghép | Đã có ở đâu |
|---|---|
| **Sinh dataset bằng LLM** | `internal/ai/llm` — `GenerateStructured` ép JSON qua forced tool-use, mặc định `claude-haiku-4-5` (rẻ, đủ tốt cho việc sinh query) |
| **Hai đường retrieval cùng signature** | `retriever.Retrieve` và `retriever.HybridSearch` cùng trả `(ctx, query, topK) → []Result` với `ChunkID` — so sánh ranking là phép so trên slice id |
| **Metrics** | `pkg/util/eval` — `Judge` (Hit@K, reciprocal rank) và `Summarize` (Recall@K, MRR), hàm thuần có unit test, không phụ thuộc corpus |
| **Đọc corpus cho tooling** | `store.ListChunks` — lấy raw chunk để sinh dataset |

Phần còn thiếu duy nhất là CLI harness (~200 dòng) — xem thiết kế bên dưới.

## Thiết kế harness (khi build)

### Đơn vị so sánh là "strategy", không phải "search method"

Bài học từ việc soi các kỹ thuật sắp tới:

- **Contextual retrieval** không phải một cách query khác — nó thay đổi *cái được
  index* (thêm context của document vào chunk trước khi embed). Benchmark nó cần một
  **corpus variant** được index lại, chạy cùng bộ query trên cả hai corpus.
- **Prompt caching** cải thiện **cost/latency, không cải thiện độ chính xác
  retrieval** — nếu đo thì thuộc trục benchmark khác (latency/cost), đừng ép vào
  harness đo Recall/MRR.

Vậy: **strategy = (corpus variant × query path)**. Harness nhận danh sách strategy
pluggable:

```go
type Strategy func(ctx context.Context, query string, topK int) ([]int64, error)

strategies := map[string]Strategy{
    "vector-only": ...,  // retriever.Retrieve
    "hybrid-rrf":  ...,  // retriever.HybridSearch
    // sau này: "contextual+hybrid" — đăng ký thêm 1 hàm, không viết lại harness
}
```

### Dataset: Haiku sinh 2 kiểu query mỗi chunk

Mỗi chunk trong corpus sinh 2 query qua forced tool-use:

- **keyword_query** — tái dùng gần nguyên văn các từ/cụm đặc trưng, hiếm gặp của
  chunk (tên app, số tiền, thuật ngữ riêng của chiêu trò) → stress nhánh lexical.
- **semantic_query** — diễn giải lại tình huống bằng cách nói khác, *không* dùng lại
  từ đặc trưng → stress nhánh vector.

Chấm điểm: query phải tìm lại được đúng chunk gốc trong top-K → Recall@K, MRR, tách
theo (kiểu query × strategy). Chính phép tách này mới cho thấy nhánh keyword có "trả
tiền vé" hay không — số liệu gộp sẽ che mất.

**Lưu ý về vòng đời dataset**: dataset tham chiếu `chunk_id`, chỉ ổn định khi corpus
append-only. Regenerate dataset theo từng snapshot corpus; không commit một dataset
"chuẩn" cố định.

## Vì sao CHƯA chạy benchmark (quyết định 2026-07)

Corpus hiện tại: ~50 bài crawl, **101 chunk**, độ đa dạng nội dung còn thấp. Ba lý do
benchmark lúc này không những vô ích mà còn nguy hiểm:

1. **Ceiling effect.** Quá ít distractor — vector search gần như luôn đưa đúng chunk
   vào top-5. Kết quả dự đoán được: Recall@5 ≈ 1.0 cho *cả hai* phương pháp. Bảng số
   liệu đẹp, thông tin bằng 0.
2. **Nguy cơ kết luận sai.** "Hybrid không hơn vector" trên corpus 101 chunk là
   artifact của kích thước corpus, không phải thuộc tính thuật toán — nhưng người đọc
   số liệu có thể đề xuất gỡ hybrid. Lợi thế của nhánh keyword (từ hiếm bị embedding
   pha loãng) chỉ xuất hiện khi có nhiều chunk na ná cạnh tranh nhau.
3. **Không đủ sức mạnh thống kê.** ~30–50 query mỗi kiểu, chênh 1–2 hit là nhiễu.

Đã kiểm chứng thực nghiệm (smoke test `cmd/seed -compare`, PR #5): 4 query thử —
gồm cả keyword-heavy lẫn semantic-heavy — hybrid không đổi ranking nào so với
vector-only trên corpus này. Trong đó 2 query không có match lexical nào trong corpus
(nhánh keyword trả rỗng, hybrid degrade êm về vector — đúng thiết kế).

## Điều kiện kích hoạt: khi nào quay lại chạy

Chạy benchmark khi **một trong hai** điều kiện xảy ra:

- **Corpus đạt ~500–1000 chunk** với scam-type đa dạng (đủ distractor để phân biệt
  hai phương pháp và đủ n cho thống kê); HOẶC
- **Sắp có quyết định cần số liệu**: adopt contextual retrieval, đổi embedding model,
  tune `candidateTopK`/`rrfK` của RRF.

Nguyên tắc: benchmark khi có câu hỏi cần trả lời, không phải benchmark để có benchmark.

## Cách chạy (khi harness đã build)

```bash
# 1. Sinh dataset từ snapshot corpus hiện tại (cần ANTHROPIC_API_KEY)
VOYAGE_API_KEY=... ANTHROPIC_API_KEY=... go run ./cmd/benchmark -gen

# 2. Chấm điểm mọi strategy trên dataset đó (chỉ cần VOYAGE_API_KEY)
VOYAGE_API_KEY=... go run ./cmd/benchmark -k 5
```

Kết quả persist theo timestamp trong `benchmark/results/` để so sánh giữa các lần chạy.
