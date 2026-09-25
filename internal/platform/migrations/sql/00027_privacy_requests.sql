-- +goose Up
-- Personal data exports and erasures, queued by the API and processed by app scheduler.
-- storage_key points at an export archive in object storage until it expires.
CREATE TABLE privacy_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('export', 'erase')),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    storage_key text,
    expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text CHECK (char_length(last_error) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CHECK (storage_key IS NULL OR kind = 'export')
);
-- At most one open request per user and kind.
CREATE UNIQUE INDEX privacy_requests_open_unique ON privacy_requests (user_id, kind)
    WHERE status IN ('pending', 'running');
CREATE INDEX privacy_requests_user_idx ON privacy_requests (user_id, kind, created_at DESC);
CREATE INDEX privacy_requests_queue_idx ON privacy_requests (created_at)
    WHERE status IN ('pending', 'running');
CREATE INDEX privacy_requests_archive_idx ON privacy_requests (expires_at)
    WHERE storage_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS privacy_requests;
