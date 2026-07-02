-- +goose Up
-- Lexical search column for hybrid (keyword + vector) retrieval — the keyword arm
-- of retriever.HybridSearch queries this via ts_rank/@@.
--
-- Config 'simple': Vietnamese has no built-in Postgres text-search dictionary, so we
-- avoid stemming and just fold/tokenise on whitespace and punctuation. The two-argument
-- to_tsvector(regconfig, text) is IMMUTABLE (the config is a fixed constant) — that is
-- what a GENERATED column requires; the one-argument form is only STABLE and would be
-- rejected here. Existing rows are populated automatically on ADD COLUMN, and the value
-- stays in sync on every insert/update without touching application code.
ALTER TABLE chunks ADD COLUMN content_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED;

CREATE INDEX chunks_content_tsv_gin ON chunks USING GIN (content_tsv);

-- +goose Down
DROP INDEX IF EXISTS chunks_content_tsv_gin;
ALTER TABLE chunks DROP COLUMN IF EXISTS content_tsv;
