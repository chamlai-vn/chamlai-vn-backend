# CLAUDE.md

Guidance for working in this repo. Keep it lean — update when conventions change.

## What this is

**ChậmLại.vn backend** — a RAG service that scores whether a Vietnamese message is a scam.
Pipeline: suspicious text → embed → pgvector top-k similar scam patterns → Claude scores a
`red|yellow|green` verdict as structured JSON. Go · PostgreSQL+pgvector · Voyage AI embeddings ·
Anthropic Claude API.

## Layout (actual)

```
cmd/
  api/         # HTTP API entrypoint (+ swagger)
  crawler/     # CLI: crawl scam-warning articles into the corpus
  migration/   # DB migration runner
internal/
  api/         # HTTP layer: router, handlers, middleware (v1, context, base)
  analyzer/    # core use case: text → retrieve → LLM scoring → verdict
  embedder/    # embedding providers behind a Service interface
  llm/         # Anthropic client + prompts
  repository/  # Postgres + pgvector access
  model/       # domain types
  config/      # config loading
```

Note: `README.md` describes an aspirational layout (`embedding/`, `store/`, `corpus/`); the list
above reflects the code on disk.

## Commands

```bash
docker compose up -d db   # Postgres + pgvector (pgvector/pgvector:pg17) on :5432
go run ./cmd/api          # API on :8080
go build ./...            # build everything
go test ./...             # tests
go vet ./...              # vet
make switch.local         # copy .env.local -> .env (env files: .env.<name>)
```

Git hooks via `lefthook.yml`. DB creds (local docker): user/pass/db all `chamlai`.

## Conventions

- **Embedding providers** follow one shape: `New<Provider>(cfg <Provider>Config, opts ...<Provider>Option)`,
  with zero-value defaults applied inside the constructor and **per-provider** Option types (no
  global Option). Callers go through `embedder.New(Config)` and depend only on the `Service`
  interface — never construct a provider directly. OpenAI-compatible REST providers reuse
  `doEmbed` in `internal/embedder/common.go`; providers needing different transport/auth (Bedrock,
  Vertex AI) carry it in their Config and skip `doEmbed`. See the package doc in
  `internal/embedder/embedder.go` before adding a provider.
- **Dimensions must match the DB `vector(N)` column.** A model/provider swap that changes vector
  size silently corrupts the index — `Service.Dimensions()` exists to assert against the column.

## Notes

- No tests yet. `internal/embedder` (the `doEmbed` index-mapping + length checks) is the highest-value
  place to start, testable with `httptest.Server`.
- `go build ./...` currently fails in `cmd/crawler` (`main` undeclared) — work in progress, unrelated
  to other packages. Build/test individual packages (e.g. `go build ./internal/...`) to avoid it.
