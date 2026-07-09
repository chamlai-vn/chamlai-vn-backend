-- +goose Up
-- Rebuild the corpus around structured, multi-representation documents (see
-- docs/plans/2026-07-07-001-refactor-rebuild-corpus-ingest-pipeline-plan.md).
-- Pre-production: this is a destructive drop+recreate, not an in-place ALTER —
-- editing 0002/0004 in place would rewrite already-applied migration history.
--
-- Changes vs. the 0002+0004 schema:
--   - documents gains `prevention` (reference advice, surfaced to the analyzer,
--     never chunked/embedded) and `updated_at`.
--   - chunks gains `kind` (content|query — the multi-representation discriminator;
--     see model.ChunkKind) and `updated_at`.
--   - content_tsv now generates from a `vietnamese`+`unaccent` text-search config
--     instead of bare 'simple', so accent-insensitive queries ("lua dao") still
--     match accented content ("lừa đảo"). 'simple' tokenizes on whitespace only
--     (no stemming/segmentation) and does not fold diacritics on its own.
--   - vector(1024) is unchanged and must keep matching embedder.Service.Dimensions()
--     — see the runtime assertion in internal/scam/ingest (New) added alongside
--     this migration; a provider/model swap that changes vector size silently
--     corrupts the HNSW index otherwise.
--
-- updated_at has DEFAULT now() but no trigger: the update model here is
-- hard-delete + re-ingest (see State Lifecycle Risks in the plan), not in-place
-- UPDATE, so there is currently no writer that would ever move it past
-- created_at. If an in-place UPDATE path is added later, add a BEFORE UPDATE
-- trigger at that time — a trigger is the right layer once there is one, since
-- there is deliberately no gorm hook on these pgx-native tables to do it in Go.

CREATE EXTENSION IF NOT EXISTS unaccent;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_ts_config WHERE cfgname = 'vietnamese'
    ) THEN
        CREATE TEXT SEARCH CONFIGURATION vietnamese (COPY = simple);
        ALTER TEXT SEARCH CONFIGURATION vietnamese
            ALTER MAPPING FOR asciiword, word WITH unaccent, simple;
    END IF;
END
$$;
-- +goose StatementEnd

-- unaccent() is marked STABLE (its dictionary lookup could theoretically
-- change), not IMMUTABLE, so Postgres refuses it directly inside a GENERATED
-- column expression ("generation expression is not immutable", SQLSTATE
-- 42P17). Wrap it in a trivial IMMUTABLE SQL function with a pinned
-- search_path — the standard workaround for this exact error.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION immutable_unaccent(text) RETURNS text AS $$
    SELECT unaccent('unaccent', $1)
$$ LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT SET search_path = public, pg_temp;
-- +goose StatementEnd

DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS documents;

CREATE TABLE documents (
    id         BIGSERIAL   PRIMARY KEY,
    url        TEXT        NOT NULL,
    title      TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    prevention TEXT        NOT NULL DEFAULT '',
    scam_type  TEXT        NOT NULL,
    source     TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT documents_url_key UNIQUE (url)
);

-- +goose StatementBegin
CREATE TABLE chunks (
    id          BIGSERIAL    PRIMARY KEY,
    document_id BIGINT       NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    kind        TEXT         NOT NULL CONSTRAINT chunks_kind_check CHECK (kind IN ('content', 'query')),
    content     TEXT         NOT NULL,
    embedding   VECTOR(1024) NOT NULL,
    content_tsv tsvector GENERATED ALWAYS AS (to_tsvector('vietnamese', immutable_unaccent(content))) STORED,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- ef_construction raised from the pgvector default (64) to 128 for a better
-- graph on the rebuilt corpus; this is a one-time index-build cost, not a
-- per-query cost. Query-time recall is tuned separately via the
-- hnsw.ef_search session GUC (see internal/infra/store SearchSimilar).
CREATE INDEX chunks_embedding_hnsw_cosine
    ON chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);

CREATE INDEX chunks_document_id_idx ON chunks (document_id);
CREATE INDEX chunks_content_tsv_gin ON chunks USING GIN (content_tsv);

-- +goose Down
-- Destructive rebuild: there is no meaningful downgrade target once this has
-- applied (it does not restore the pre-0005 documents/chunks shape). To fully
-- roll back the corpus schema, `goose reset` to 0001 rather than stepping down
-- through this migration alone.
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS documents;
DROP FUNCTION IF EXISTS immutable_unaccent(text);
