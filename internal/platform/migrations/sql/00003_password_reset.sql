-- +goose Up
CREATE TABLE password_reset_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    jti text NOT NULL,
    token_hash text NOT NULL,
    purpose text NOT NULL DEFAULT 'password_reset'
        CHECK (purpose IN ('password_reset', 'email_change')),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX password_reset_tokens_jti_unique ON password_reset_tokens (jti);
CREATE UNIQUE INDEX password_reset_tokens_hash_unique ON password_reset_tokens (token_hash);
CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id, purpose);

-- +goose Down
DROP TABLE IF EXISTS password_reset_tokens;
