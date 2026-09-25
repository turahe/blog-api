-- +goose Up
-- In-app notifications: the inbox behind GET /me/notifications and the SSE stream.
-- title/body are the web copy and preview the SSE line, rendered when the row is written.
-- dedupe_key makes a notice idempotent per user (for example one reply notice per reply).
CREATE TABLE notifications (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type text NOT NULL,
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    preview text NOT NULL DEFAULT '',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    actor_user_id bigint REFERENCES users(id) ON DELETE SET NULL,
    dedupe_key text,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
CREATE UNIQUE INDEX notifications_user_dedupe_unique ON notifications (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS notifications;
