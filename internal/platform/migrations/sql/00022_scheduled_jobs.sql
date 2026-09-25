-- +goose Up
-- Last run of each app scheduler job. Replicas take a per-job advisory lock, then skip a job
-- whose last start is more recent than its interval, so each run happens once.
CREATE TABLE scheduled_job_runs (
    name             text PRIMARY KEY,
    last_started_at  timestamptz NOT NULL,
    last_finished_at timestamptz,
    last_error       text,
    runs             bigint NOT NULL DEFAULT 0
);

CREATE INDEX refresh_sessions_expires_idx ON refresh_sessions (expires_at);
CREATE INDEX password_reset_tokens_expires_idx ON password_reset_tokens (expires_at);

-- +goose Down
DROP INDEX IF EXISTS password_reset_tokens_expires_idx;
DROP INDEX IF EXISTS refresh_sessions_expires_idx;
DROP TABLE IF EXISTS scheduled_job_runs;
