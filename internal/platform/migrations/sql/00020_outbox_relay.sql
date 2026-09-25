-- +goose Up
-- The relay claims due rows with FOR UPDATE SKIP LOCKED, retries with backoff through
-- next_attempt_at, and parks a row with failed_at after the last attempt.
ALTER TABLE outbox_events
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN failed_at timestamptz;

DROP INDEX IF EXISTS outbox_unpublished_idx;
CREATE INDEX outbox_due_idx ON outbox_events (next_attempt_at, id)
    WHERE published_at IS NULL AND failed_at IS NULL;
CREATE INDEX outbox_published_idx ON outbox_events (published_at)
    WHERE published_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS outbox_published_idx;
DROP INDEX IF EXISTS outbox_due_idx;
CREATE INDEX outbox_unpublished_idx ON outbox_events (occurred_at)
    WHERE published_at IS NULL;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS next_attempt_at;
