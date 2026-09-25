-- +goose Up
-- Rollup exports requested from the admin dashboard and built by app scheduler. first_day and
-- last_day are the whole periods exported, read in timezone (the site time zone at request
-- time). storage_key points at the ZIP archive in object storage until it expires.
CREATE TABLE analytics_exports (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    first_day date NOT NULL,
    last_day date NOT NULL,
    timezone text NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 64),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    storage_key text,
    size_bytes bigint,
    expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text CHECK (char_length(last_error) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CHECK (first_day <= last_day)
);
-- At most one open export per user.
CREATE UNIQUE INDEX analytics_exports_open_unique ON analytics_exports (user_id)
    WHERE status IN ('pending', 'running');
CREATE INDEX analytics_exports_user_idx ON analytics_exports (user_id, created_at DESC);
CREATE INDEX analytics_exports_queue_idx ON analytics_exports (created_at)
    WHERE status IN ('pending', 'running');
CREATE INDEX analytics_exports_archive_idx ON analytics_exports (expires_at)
    WHERE storage_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS analytics_exports;
