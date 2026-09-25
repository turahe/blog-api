-- +goose Up
-- Staff impersonation. A session backs one short-lived access token whose sub is the target
-- and whose act claim is the impersonator; every request with that token re-checks the row.
CREATE TABLE impersonation_sessions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    actor_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state text NOT NULL DEFAULT 'active'
        CHECK (state IN ('active', 'exited', 'expired', 'revoked')),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 10 AND 255),
    ip_address text,
    user_agent text,
    started_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    ended_at timestamptz,
    end_reason text CHECK (end_reason IN ('manual_exit', 'expired', 'policy')),
    CHECK (actor_id <> target_id),
    CHECK (expires_at > started_at),
    CHECK ((state = 'active') = (ended_at IS NULL))
);
-- At most one active session per impersonator.
CREATE UNIQUE INDEX impersonation_sessions_active_actor ON impersonation_sessions (actor_id)
    WHERE state = 'active';
CREATE INDEX impersonation_sessions_expiry_idx ON impersonation_sessions (expires_at)
    WHERE state = 'active';
CREATE INDEX impersonation_sessions_target_idx ON impersonation_sessions (target_id, started_at DESC);

-- +goose Down
DROP TABLE IF EXISTS impersonation_sessions;
