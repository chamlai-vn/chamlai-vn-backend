-- +goose Up
-- Corpus of scam-warning articles (documents) split into embedded chunks.
-- embedding is vector(1024) to match Voyage's voyage-3.5 default — keep this in
-- sync with embedder.Service.Dimensions() or the index silently corrupts.

CREATE TABLE documents (
    id         BIGSERIAL PRIMARY KEY,
    url        TEXT        NOT NULL,
    title      TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    scam_type  TEXT        NOT NULL,
    source     TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE chunks (
    id          BIGSERIAL    PRIMARY KEY,
    document_id BIGINT       NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    content     TEXT         NOT NULL,
    embedding   VECTOR(1024) NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- HNSW index for cosine distance (<=>), the operator SearchSimilar uses.
CREATE INDEX chunks_embedding_hnsw_cosine
    ON chunks USING hnsw (embedding vector_cosine_ops);

CREATE INDEX chunks_document_id_idx ON chunks (document_id);

-- +goose Down
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS documents;
