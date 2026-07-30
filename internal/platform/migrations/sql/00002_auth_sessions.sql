-- +goose Up
CREATE TABLE refresh_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id uuid NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    replaced_by uuid REFERENCES refresh_sessions(id) ON DELETE SET NULL,
    user_agent text,
    ip_address text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX refresh_sessions_token_hash_unique ON refresh_sessions (token_hash);
CREATE INDEX refresh_sessions_user_family_idx ON refresh_sessions (user_id, family_id);
CREATE INDEX refresh_sessions_active_idx ON refresh_sessions (user_id, expires_at)
    WHERE revoked_at IS NULL;

ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name text;
UPDATE users SET full_name = COALESCE(full_name, username) WHERE full_name IS NULL;
ALTER TABLE users ALTER COLUMN full_name SET NOT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS login_count integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS login_count;
ALTER TABLE users DROP COLUMN IF EXISTS last_login_at;
ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS full_name;
DROP TABLE IF EXISTS refresh_sessions;
