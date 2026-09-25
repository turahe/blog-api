-- +goose Up
-- Audit log request context and activity lookups. An entry belongs to a user's
-- activity when the user is the actor or the entry targets the user's account
-- (resource_type = 'user'); category is NULL for admin-only entries.
ALTER TABLE audit_logs
    ADD COLUMN category text,
    ADD COLUMN result text NOT NULL DEFAULT 'success',
    ADD COLUMN ip_address text,
    ADD COLUMN user_agent text,
    ADD COLUMN request_id text;
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_id, occurred_at DESC);
CREATE INDEX audit_logs_resource_idx ON audit_logs (resource_type, resource_id, occurred_at DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_logs_resource_idx;
DROP INDEX IF EXISTS audit_logs_actor_idx;
ALTER TABLE audit_logs
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS user_agent,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS result,
    DROP COLUMN IF EXISTS category;
