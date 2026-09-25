-- +goose Up
-- Append-only history of every post write: a full snapshot of the post's content, meta,
-- tags, and media at that moment, plus the diff against the previous revision. Rows are
-- never updated; they stay until an admin prunes them. Category and cover are snapshot
-- as UUIDs without foreign keys so a revision survives the referenced row's deletion.
CREATE TABLE post_revisions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    post_id bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    revision_number integer NOT NULL CHECK (revision_number > 0),
    revision_type text NOT NULL CHECK (revision_type IN
        ('create', 'update', 'publish', 'unpublish', 'archive', 'delete', 'undelete', 'restore')),
    title text NOT NULL,
    slug text NOT NULL,
    excerpt text NOT NULL DEFAULT '',
    content text NOT NULL DEFAULT '',
    status text NOT NULL,
    comment_policy text NOT NULL,
    category_uuid uuid,
    cover_image_media_uuid uuid,
    tags_snapshot jsonb NOT NULL DEFAULT '[]',
    media_snapshot jsonb NOT NULL DEFAULT '[]',
    seo_snapshot jsonb NOT NULL DEFAULT '{}',
    changed_fields jsonb NOT NULL DEFAULT '[]',
    diff jsonb NOT NULL DEFAULT '{}',
    changelog text NOT NULL DEFAULT '',
    editor_note text NOT NULL DEFAULT '',
    author_id bigint REFERENCES users(id) ON DELETE SET NULL,
    restore_from_revision_id bigint REFERENCES post_revisions(id) ON DELETE SET NULL,
    impersonator_id bigint REFERENCES users(id) ON DELETE SET NULL,
    request_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (post_id, revision_number)
);
CREATE INDEX post_revisions_post_created_idx ON post_revisions (post_id, created_at DESC, id DESC);
CREATE INDEX post_revisions_author_idx ON post_revisions (author_id);
CREATE INDEX post_revisions_restore_from_idx ON post_revisions (restore_from_revision_id);

-- +goose Down
DROP TABLE IF EXISTS post_revisions;
