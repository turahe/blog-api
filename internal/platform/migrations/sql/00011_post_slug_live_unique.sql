-- +goose Up
-- Soft-deleted posts release their slug: uniqueness only applies to live rows, so a new
-- post can reuse it and a restore resolves any clash with a numeric suffix.
ALTER TABLE posts DROP CONSTRAINT posts_slug_key;
CREATE UNIQUE INDEX posts_slug_live_key ON posts (slug) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS posts_slug_live_key;
ALTER TABLE posts ADD CONSTRAINT posts_slug_key UNIQUE (slug);
