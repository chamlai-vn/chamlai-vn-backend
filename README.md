<p align="right">
  <a href="README-en.md"><img src="https://flagcdn.com/20x15/gb.png" width="20" height="15" alt="Cờ Anh"> English</a>
  &nbsp;|&nbsp;
  <a href="README.md"><img src="https://flagcdn.com/20x15/vn.png" width="20" height="15" alt="Cờ Việt Nam"> Tiếng Việt</a>
</p>

# ChậmLại.vn 🛡️

**Trợ lý AI giúp người Việt — đặc biệt là người lớn tuổi — kiểm tra một tin nhắn đáng ngờ có phải lừa đảo hay không.**

Dán bất kỳ văn bản đáng ngờ nào (SMS, tin nhắn Zalo, "hợp đồng kỳ nghỉ", việc nhẹ lương cao...) và nhận về đánh giá tiềm tàng, các dấu hiệu đáng ngờ bằng ngôn ngữ đời thường, và việc nên làm tiếp theo. Không cần đăng nhập.

> ⚠️ **Lưu ý**: ChậmLại.vn là công cụ tham khảo, không thay thế tư vấn pháp lý. Công cụ không bao giờ khẳng định "an toàn 100%" và không kết luận về cá nhân hay tổ chức cụ thể.

## Cách hoạt động

```
văn bản đáng ngờ
      │
      ▼
 embed (Voyage AI) ──► pgvector: top-k scam pattern tương tự
      │                          │
      ▼                          ▼
 Claude (Anthropic API) ◄── context đã retrieve
      │
      ▼
 kết quả JSON có cấu trúc
 { risk: đỏ|vàng|xanh, red_flags[], next_actions[], patterns[] }
```

RAG trên corpus bài cảnh báo lừa đảo đã gắn nhãn (VTV, CAND, Cục An toàn thông tin...), chấm điểm dấu hiệu lừa đảo bằng Claude.

## Vì sao chọn RAG? (3 điều cốt lõi)

**1. RAG bắt kịp kịch bản lừa đảo mới, fine-tune thì không.** Chiêu lừa ở Việt Nam thay đổi liên tục. Với RAG, chỉ cần thêm một bài cảnh báo mới vào corpus là hệ thống nhận ra pattern đó ngay — đặc biệt mạnh khi cộng đồng người dùng đóng góp dữ liệu thực tế. Fine-tune ngược lại tốn công thu thập, làm sạch, gắn nhãn dữ liệu và chi phí cao hơn nhiều, mà mỗi lần có chiêu mới lại phải train lại.

**2. Luồng dữ liệu đi thẳng từ văn bản tới verdict.** Hợp đồng/văn bản đáng ngờ → embed → query pgvector lấy scam pattern tương tự → inject vào prompt → Claude chấm điểm đỏ/vàng/xanh. Đây chính là ba bước Retrieval → Augmentation → Generation, ánh xạ trực tiếp vào các package trong `internal/`.

**3. Kết hợp semantic + lexical (BM25), và tập trung thị trường Việt Nam.** Tìm theo ý nghĩa (embedding) bắt được văn bản diễn đạt khác nhưng cùng bản chất lừa; tìm theo từ khóa (BM25) bắt được tên chiêu trò, số hotline giả, cụm từ đặc trưng. Hai cách bổ trợ nhau nên dùng cả hai thay vì chỉ semantic. Không cố tổng quát hóa cho thị trường khác lúc này — mỗi quốc gia có kịch bản lừa đảo riêng, làm tốt cho Việt Nam trước đã.

## Công nghệ

Go · PostgreSQL + pgvector · Voyage AI embeddings · Anthropic Claude API

## Cấu trúc dự án

```
cmd/
  server/      # entrypoint HTTP API
  crawler/     # CLI: crawl bài cảnh báo lừa đảo vào corpus
internal/
  analyzer/    # use case lõi: văn bản → retrieve → LLM scoring → verdict
  corpus/      # ingest: crawl, làm sạch, chunk, gắn nhãn loại scam
  embedding/   # client Voyage AI
  llm/         # client Anthropic + prompt templates
  store/       # Postgres + pgvector (pgx)
  server/      # tầng HTTP: router, handlers, middleware
migrations/    # schema SQL (tự chạy khi khởi động DB lần đầu)
```

## Chạy thử

```bash
cp .env.example .env   # điền API keys
make db-up             # Postgres + pgvector qua Docker
make run               # API tại :8080
curl localhost:8080/health
```

## Lộ trình

- [x] Skeleton repo, setup Postgres + pgvector
- [ ] Corpus: index 50+ bài cảnh báo lừa đảo đã gắn nhãn
- [ ] `/analyze` end-to-end: hybrid retrieval + reranking + LLM scoring
- [ ] Eval baseline (precision/recall trên golden dataset)
- [ ] Web UI: ô dán văn bản → đèn giao thông, chữ to, mobile-friendly
- [ ] Streaming, prompt caching, contextual retrieval
- [ ] Deploy public
