<p align="right">
  <a href="README-en.md"><img src="https://flagcdn.com/20x15/gb.png" width="20" height="15" alt="English flag"> English</a>
  &nbsp;|&nbsp;
  <a href="README.md"><img src="https://flagcdn.com/20x15/vn.png" width="20" height="15" alt="Vietnamese flag"> Tiếng Việt</a>
</p>

# ChậmLại.vn 🛡️

**AI assistant that helps Vietnamese users — especially the elderly — check whether a suspicious message is a scam.**

Paste any suspicious text (SMS, Zalo message, "vacation contract", "easy job, high pay"...) and get a traffic-light risk verdict, plain-language red flags, and what to do next. No login required.

> ⚠️ **Disclaimer**: ChậmLại.vn is a reference tool, not legal advice. It never declares anything "100% safe" and never makes claims about specific individuals or organizations.

## How it works

```
suspicious text
      │
      ▼
 embed (Voyage AI) ──► pgvector: top-k similar scam patterns
      │                          │
      ▼                          ▼
 Claude (Anthropic API) ◄── retrieved context
      │
      ▼
 structured JSON verdict
 { risk: red|yellow|green, red_flags[], next_actions[], patterns[] }
```

RAG over a labeled corpus of Vietnamese scam-warning articles (VTV, CAND, Cục An toàn thông tin...), with scam-signal scoring by Claude.

## Tech stack

Go · PostgreSQL + pgvector · Voyage AI embeddings · Anthropic Claude API

## Project layout

```
cmd/
  server/      # HTTP API entrypoint
  crawler/     # CLI: crawl scam-warning articles into the corpus
internal/
  analyzer/    # core use case: text → retrieve → LLM scoring → verdict
  corpus/      # ingest: crawl, clean, chunk, label by scam type
  embedding/   # Voyage AI client
  llm/         # Anthropic client + prompt templates
  store/       # Postgres + pgvector (pgx)
  server/      # HTTP layer: router, handlers, middleware
migrations/    # SQL schema (auto-applied on first db start)
```

## Getting started

```bash
cp .env.example .env   # fill in API keys
make db-up             # Postgres + pgvector via Docker
make run               # API on :8080
curl localhost:8080/health
```

## Roadmap

- [x] Repo skeleton, Postgres + pgvector setup
- [ ] Corpus: 50+ labeled scam-warning articles indexed
- [ ] `/analyze` end-to-end: hybrid retrieval + reranking + LLM scoring
- [ ] Eval baseline (precision/recall on golden dataset)
- [ ] Web UI: paste box → traffic light, large text, mobile-friendly
- [ ] Streaming, prompt caching, contextual retrieval
- [ ] Public deploy
