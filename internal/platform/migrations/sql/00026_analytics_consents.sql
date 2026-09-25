-- +goose Up
-- A consent subject is one browser's pseudonymous consent identity. The client holds a
-- random bearer token; only its SHA-256 hash is stored. user_id is set only while the
-- subject grants authenticated_analytics.
CREATE TABLE consent_subjects (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    token_hash text NOT NULL UNIQUE CHECK (char_length(token_hash) = 64),
    user_id bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX consent_subjects_user_idx ON consent_subjects (user_id) WHERE user_id IS NOT NULL;

-- One row per subject and purpose holding the current decision and the policy version
-- the subject saw. Changes are also recorded as analytics.consent.* outbox events.
CREATE TABLE analytics_consents (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    subject_id bigint NOT NULL REFERENCES consent_subjects(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('analytics', 'authenticated_analytics')),
    status text NOT NULL CHECK (status IN ('granted', 'rejected', 'withdrawn')),
    policy_version text NOT NULL CHECK (char_length(policy_version) BETWEEN 1 AND 32),
    decided_at timestamptz NOT NULL,
    withdrawn_at timestamptz,
    UNIQUE (subject_id, purpose)
);

-- +goose Down
DROP TABLE IF EXISTS analytics_consents;
DROP TABLE IF EXISTS consent_subjects;
