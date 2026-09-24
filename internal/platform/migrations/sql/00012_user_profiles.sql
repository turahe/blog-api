-- +goose Up
-- 1:1 extensions of users, keyed by user_id. Rows are created on first write;
-- readers treat a missing row as the column defaults.
CREATE TABLE user_profiles (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name text CHECK (char_length(display_name) BETWEEN 1 AND 60),
    bio text NOT NULL DEFAULT '' CHECK (char_length(bio) <= 4000),
    contact_website text CHECK (char_length(contact_website) <= 2048),
    contact_location text CHECK (char_length(contact_location) <= 120),
    social_links jsonb NOT NULL DEFAULT '{}'::jsonb,
    locale text NOT NULL DEFAULT 'en_US',
    timezone text NOT NULL DEFAULT 'UTC',
    marketing_consent boolean NOT NULL DEFAULT false,
    marketing_consent_updated_at timestamptz,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_profiles_display_name_unique
    ON user_profiles (lower(display_name)) WHERE display_name IS NOT NULL;

CREATE TABLE user_privacy_settings (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    visibility_profile text NOT NULL DEFAULT 'public'
        CHECK (visibility_profile IN ('public', 'unlisted', 'private')),
    visibility_email boolean NOT NULL DEFAULT false,
    visibility_contact boolean NOT NULL DEFAULT true,
    search_allow_indexing boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Pending address for purpose = 'email_change' tokens.
ALTER TABLE password_reset_tokens ADD COLUMN new_email text;

-- +goose Down
ALTER TABLE password_reset_tokens DROP COLUMN IF EXISTS new_email;
DROP TABLE IF EXISTS user_privacy_settings;
DROP TABLE IF EXISTS user_profiles;
