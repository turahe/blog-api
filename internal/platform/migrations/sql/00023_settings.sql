-- +goose Up
-- Admin-managed, non-secret settings. The key catalogue (type, category, sensitivity,
-- default, validation) lives in code; a row exists only once a key was changed from its
-- default. version guards concurrent updates: writers update WHERE version = expected.
CREATE TABLE settings (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    key text NOT NULL UNIQUE CHECK (char_length(key) <= 128),
    value jsonb NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One row per applied change, kept until an admin prunes it. previous_value is the value
-- in effect before the change (the coded default for a key's first change).
CREATE TABLE settings_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    setting_key text NOT NULL,
    previous_value jsonb,
    new_value jsonb NOT NULL,
    version bigint NOT NULL,
    changed_by bigint REFERENCES users(id) ON DELETE SET NULL,
    request_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX settings_history_created_idx ON settings_history (created_at DESC, id DESC);
CREATE INDEX settings_history_key_created_idx ON settings_history (setting_key, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS settings_history;
DROP TABLE IF EXISTS settings;
