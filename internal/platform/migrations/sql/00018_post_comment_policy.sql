-- +goose Up
-- Per-post comment policy: open (guests allowed when enabled site-wide), authenticated
-- (signed-in users only), read_only (existing comments shown, no new ones), disabled
-- (comments hidden and closed).
ALTER TABLE posts
    ADD COLUMN comment_policy text NOT NULL DEFAULT 'open'
        CONSTRAINT posts_comment_policy_check
        CHECK (comment_policy IN ('open', 'authenticated', 'read_only', 'disabled'));

-- +goose Down
ALTER TABLE posts DROP COLUMN IF EXISTS comment_policy;
