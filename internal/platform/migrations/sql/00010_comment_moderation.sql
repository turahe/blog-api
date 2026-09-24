-- +goose Up
ALTER TABLE comments
    ADD COLUMN moderated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN moderation_reason text,
    ADD COLUMN moderated_at timestamptz;

-- The admin queue lists by status across all posts, oldest first.
CREATE INDEX comments_status_created_idx ON comments (status, created_at);

-- Append-only: rows are never updated. comment_uuid keeps history readable after a hard
-- delete nulls comment_id.
CREATE TABLE comment_moderation_log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    comment_id bigint REFERENCES comments(id) ON DELETE SET NULL,
    comment_uuid uuid NOT NULL,
    moderator_id bigint REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL CHECK (action IN ('approve', 'reject', 'spam', 'restore', 'hard_delete')),
    from_status text NOT NULL,
    to_status text NOT NULL,
    reason text,
    notify_author boolean NOT NULL DEFAULT false,
    before_state jsonb,
    after_state jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comment_moderation_log_comment_idx ON comment_moderation_log (comment_uuid, created_at);

-- The seeder now grants comment.moderate and comment.delete; the old key was never enforced.
DELETE FROM casbin_rules WHERE ptype = 'p' AND v1 = 'comments.moderate';
DELETE FROM permissions WHERE key = 'comments.moderate';

-- +goose Down
DROP TABLE IF EXISTS comment_moderation_log;
DROP INDEX IF EXISTS comments_status_created_idx;

ALTER TABLE comments
    DROP COLUMN IF EXISTS moderated_at,
    DROP COLUMN IF EXISTS moderation_reason,
    DROP COLUMN IF EXISTS moderated_by;
