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
  api/            # HTTP API entrypoint (+ swagger)
  crawler/        # CLI: build the scam corpus (crawl urls + local files → ingest)
  seed/           # CLI: manual end-to-end smoke test of the RAG retrieval path
  migration/      # DB migration runner
internal/
  ai/
    embedder/     # embedding providers behind a Service interface
    llm/          # Anthropic client + prompts
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
config/           # config loading (top-level package `config`, not internal/)
migrations/       # schema SQL, applied via cmd/migration
```

Note: `README.md`'s "Cấu trúc dự án" is kept roughly in sync with this; the list above is the
source of truth for the code on disk.

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

- **File layout within a package** (mirrors the PeopleCoral sister repo). Application-service
  packages — those that wire collaborators and expose a `New()` (e.g. `internal/scam/ingest`,
  `internal/scam/analyzer`) — split by file role, not by declaration kind:
  - `service.go` — the service struct, its `New()` constructor, `Option`s, and any narrow
    dependency interfaces. **This is the package's entry point**; start reading here.
  - `type.go` — request/result/domain DTOs the package exposes.
  - `<feature>.go` — the behaviour/methods (e.g. `ingest.go` holds `IndexDocument`).

  Provider/library-abstraction packages are the exception: `New()` lives in the file named after
  the package (`internal/ai/embedder/embedder.go`), with the `Service` interface in `service.go`.
  Don't force a `type.go` on small, cohesive packages with one obvious home for their types.
- **Embedding providers** follow one shape: `New<Provider>(cfg <Provider>Config, opts ...<Provider>Option)`,
  with zero-value defaults applied inside the constructor and **per-provider** Option types (no
  global Option). Callers go through `embedder.New(Config)` and depend only on the `Service`
  interface — never construct a provider directly. OpenAI-compatible REST providers reuse
  `doEmbed` in `internal/ai/embedder/common.go`; providers needing different transport/auth (Bedrock,
  Vertex AI) carry it in their Config and skip `doEmbed`. See the package doc in
  `internal/ai/embedder/embedder.go` before adding a provider.
- **Dimensions must match the DB `vector(N)` column.** A model/provider swap that changes vector
  size silently corrupts the index — `Service.Dimensions()` exists to assert against the column.

## Notes

- `go build ./...` and `go test ./...` both pass across the tree.
- Test coverage exists in `internal/ai/embedder`, `internal/scam/{analyzer,crawler,ingest,retriever}`.
  `internal/ai/embedder` (the `doEmbed` index-mapping + length checks) remains the highest-value
  area, testable with `httptest.Server`.
