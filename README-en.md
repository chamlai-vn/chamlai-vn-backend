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

## Why RAG? (3 key takeaways)

**1. RAG keeps up with new scam scenarios; fine-tuning can't.** Scam tactics in Vietnam change constantly. With RAG, adding a single new warning article to the corpus lets the system recognize that pattern immediately — especially powerful when the user community contributes real-world data. Fine-tuning, by contrast, is expensive and requires collecting, cleaning, and labeling data, then retraining every time a new tactic appears.

**2. The data flow runs straight from text to verdict.** Suspicious contract/text → embed → query pgvector for similar scam patterns → inject into the prompt → Claude scores red/yellow/green. This is exactly the Retrieval → Augmentation → Generation pipeline, mapped directly onto the packages under `internal/`.

**3. Combine semantic + lexical (BM25), and focus on the Vietnamese market.** Semantic search (embeddings) catches text that is worded differently but is the same kind of scam; lexical search (BM25) catches tactic names, fake hotline numbers, and characteristic phrases. The two complement each other, so we use both rather than semantic alone. We won't try to generalize to other markets yet — each country has its own scam playbook, so we nail Vietnam first.

## Tech stack

Go · PostgreSQL + pgvector · Voyage AI embeddings · Anthropic Claude API

## Project layout

```
cmd/
  api/            # HTTP API entrypoint (+ swagger)
  crawler/        # CLI: build the scam corpus (crawl urls + local files → ingest)
  seed/           # CLI: manual end-to-end smoke test of the RAG retrieval path
  migration/      # DB migration runner
internal/
  ai/
    embedder/     # embedding providers behind a Service interface (Voyage, Azure...)
    llm/          # Anthropic client + prompt templates
  scam/           # the RAG domain, one package per pipeline stage
    ingest/       # crawl output → chunk → embed → store
    retriever/    # query text → pgvector top-k similar scam patterns
    analyzer/     # core use case: text → retrieve → LLM scoring → verdict
    crawler/      # fetch + parse + rule-based scam labelling
  infra/
    store/        # PostgreSQL + pgvector data-access (single pgxpool.Pool)
    repository/   # relational/auth repositories
  api/            # HTTP layer: base, context (root, v1, v2 scaffolded)
  model/          # domain types
config/           # config loading
migrations/       # SQL schema, applied via cmd/migration
```

## Getting started

```bash
make switch.local        # copy .env.local -> .env, then fill in API keys
docker compose up -d db  # Postgres + pgvector via Docker (:5432)
make migrate.local       # apply migrations
go run ./cmd/api         # API on :8080
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
