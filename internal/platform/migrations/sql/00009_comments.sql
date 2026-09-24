-- +goose Up
ALTER TABLE comments RENAME COLUMN user_id TO author_id;
ALTER TABLE comments RENAME COLUMN body TO content;

-- author_name/author_email identify guest commenters; ip_hash is SHA-256, never the plain IP.
-- Soft-deleted rows stay as thread placeholders, so depth and parent links survive deletion.
ALTER TABLE comments
    ADD COLUMN author_name text,
    ADD COLUMN author_email text,
    ADD COLUMN ip_hash text,
    ADD COLUMN user_agent text,
    ADD COLUMN depth smallint NOT NULL DEFAULT 0 CHECK (depth BETWEEN 0 AND 5),
    ADD COLUMN upvote_count integer NOT NULL DEFAULT 0 CHECK (upvote_count >= 0),
    ADD COLUMN flag_count integer NOT NULL DEFAULT 0 CHECK (flag_count >= 0),
    ADD COLUMN edited_at timestamptz,
    ADD COLUMN deleted_by bigint REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE comments DROP CONSTRAINT comments_status_check;
ALTER TABLE comments ADD CONSTRAINT comments_status_check
    CHECK (status IN ('pending', 'approved', 'flagged', 'spam', 'rejected', 'deleted'));

CREATE INDEX comments_post_roots_idx ON comments (post_id, created_at) WHERE parent_id IS NULL;
CREATE INDEX comments_parent_created_idx ON comments (parent_id, created_at) WHERE parent_id IS NOT NULL;
CREATE INDEX comments_author_created_idx ON comments (author_id, created_at) WHERE author_id IS NOT NULL;

-- One flag per identity: the user when signed in, otherwise SHA-256(IP + user agent).
CREATE TABLE comment_flags (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    comment_id bigint NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    reporter_user_id bigint REFERENCES users(id) ON DELETE SET NULL,
    reporter_ip_hash text,
    reason_code text NOT NULL
        CHECK (reason_code IN ('spam', 'abuse', 'hate', 'harassment', 'doxx', 'self_harm',
                               'copyright', 'impersonation', 'illegal', 'other')),
    details text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (reporter_user_id IS NOT NULL OR reporter_ip_hash IS NOT NULL)
);
CREATE UNIQUE INDEX comment_flags_user_unique ON comment_flags (comment_id, reporter_user_id)
    WHERE reporter_user_id IS NOT NULL;
CREATE UNIQUE INDEX comment_flags_guest_unique ON comment_flags (comment_id, reporter_ip_hash)
    WHERE reporter_user_id IS NULL;

CREATE TABLE comment_upvotes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    comment_id bigint NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    voter_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (comment_id, voter_user_id)
);

-- +goose Down
DROP TABLE IF EXISTS comment_upvotes;
DROP TABLE IF EXISTS comment_flags;

DROP INDEX IF EXISTS comments_author_created_idx;
DROP INDEX IF EXISTS comments_parent_created_idx;
DROP INDEX IF EXISTS comments_post_roots_idx;

UPDATE comments SET status = 'pending' WHERE status = 'flagged';
ALTER TABLE comments DROP CONSTRAINT comments_status_check;
ALTER TABLE comments ADD CONSTRAINT comments_status_check
    CHECK (status IN ('pending', 'approved', 'spam', 'rejected', 'deleted'));

ALTER TABLE comments
    DROP COLUMN IF EXISTS deleted_by,
    DROP COLUMN IF EXISTS edited_at,
    DROP COLUMN IF EXISTS flag_count,
    DROP COLUMN IF EXISTS upvote_count,
    DROP COLUMN IF EXISTS depth,
    DROP COLUMN IF EXISTS user_agent,
    DROP COLUMN IF EXISTS ip_hash,
    DROP COLUMN IF EXISTS author_email,
    DROP COLUMN IF EXISTS author_name;

ALTER TABLE comments RENAME COLUMN content TO body;
ALTER TABLE comments RENAME COLUMN author_id TO user_id;
