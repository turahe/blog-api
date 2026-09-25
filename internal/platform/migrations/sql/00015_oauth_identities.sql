-- +goose Up
-- Social login identities. One identity per provider per user; a provider
-- subject belongs to at most one user.
CREATE TABLE user_oauth_identities (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider text NOT NULL,
    subject text NOT NULL,
    email text,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE UNIQUE INDEX user_oauth_identities_subject_unique ON user_oauth_identities (provider, subject);
CREATE UNIQUE INDEX user_oauth_identities_user_provider_unique ON user_oauth_identities (user_id, provider);

-- +goose Down
DROP TABLE IF EXISTS user_oauth_identities;
