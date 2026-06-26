-- +goose Up
-- A document is identified by its source url. The crawler skips urls already in
-- the corpus, but that check-then-insert is not atomic under concurrency: this
-- constraint is the backstop that turns a racing duplicate into a 23505 the
-- crawler can treat as a skip, and it doubles as the index DocumentExists hits.
ALTER TABLE documents ADD CONSTRAINT documents_url_key UNIQUE (url);

-- +goose Down
ALTER TABLE documents DROP CONSTRAINT documents_url_key;
