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
