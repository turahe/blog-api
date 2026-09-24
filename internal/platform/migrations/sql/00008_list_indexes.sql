-- +goose Up
-- Public post list: matches ORDER BY published_at DESC NULLS LAST, created_at DESC
-- (PostRepository.ListPublished). The old index was NULLS FIRST, so it could not serve the sort.
CREATE INDEX IF NOT EXISTS posts_published_list_idx
    ON posts (published_at DESC NULLS LAST, created_at DESC)
    WHERE status = 'published' AND deleted_at IS NULL;
DROP INDEX IF EXISTS posts_published_at_idx;

-- Category filter on public lists, CategoryRepository.CountPosts, and the
-- ON DELETE SET NULL foreign-key check when a category is deleted.
CREATE INDEX IF NOT EXISTS posts_category_id_idx ON posts (category_id);

-- The primary key (post_id, tag_id) cannot serve lookups by tag_id alone:
-- tag filter, TagRepository.CountPosts, tag merge.
CREATE INDEX IF NOT EXISTS post_tags_tag_id_idx ON post_tags (tag_id, post_id);

-- Admin lists sorted by created_at DESC.
CREATE INDEX IF NOT EXISTS posts_created_at_idx
    ON posts (created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS media_assets_created_at_idx
    ON media_assets (created_at DESC)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS media_assets_created_at_idx;
DROP INDEX IF EXISTS posts_created_at_idx;
DROP INDEX IF EXISTS post_tags_tag_id_idx;
DROP INDEX IF EXISTS posts_category_id_idx;
CREATE INDEX IF NOT EXISTS posts_published_at_idx ON posts (published_at DESC) WHERE status = 'published';
DROP INDEX IF EXISTS posts_published_list_idx;
