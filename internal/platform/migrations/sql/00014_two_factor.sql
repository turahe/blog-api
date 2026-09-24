-- +goose Up
-- One TOTP enrollment per user. secret_ciphertext is AES-GCM under APP_ENCRYPTION_KEY;
-- confirmed_at is NULL until the user proves a working authenticator.
CREATE TABLE user_two_factor_methods (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    secret_ciphertext text NOT NULL,
    confirmed_at timestamptz,
    last_used_step bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Backup codes are stored as keyed HMACs; used_at marks a spent code.
CREATE TABLE user_two_factor_backup_codes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash text NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_two_factor_backup_codes_unique ON user_two_factor_backup_codes (user_id, code_hash);
CREATE INDEX user_two_factor_backup_codes_unused_idx ON user_two_factor_backup_codes (user_id) WHERE used_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS user_two_factor_backup_codes;
DROP TABLE IF EXISTS user_two_factor_methods;
