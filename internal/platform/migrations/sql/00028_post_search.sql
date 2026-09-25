-- +goose Up
-- Full-text search over posts. Title weighs most, then excerpt, then the first 100,000
-- characters of content (a tsvector cannot exceed 1 MB). The text search configuration is
-- baked into the expression; `app search reindex` rebuilds the column for SEARCH_LANGUAGE,
-- and must produce exactly this expression for 'simple'.
ALTER TABLE posts ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('simple'::regconfig, coalesce(title, '')), 'A') ||
    setweight(to_tsvector('simple'::regconfig, coalesce(excerpt, '')), 'B') ||
    setweight(to_tsvector('simple'::regconfig, left(content, 100000)), 'C')
) STORED;
CREATE INDEX posts_search_vector_idx ON posts USING gin (search_vector);

-- +goose Down
DROP INDEX IF EXISTS posts_search_vector_idx;
ALTER TABLE posts DROP COLUMN IF EXISTS search_vector;
