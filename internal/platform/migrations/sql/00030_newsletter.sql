-- +goose Up
-- Newsletter lists, subscribers, consent history, issues, and per-recipient deliveries.
-- Subscriber email is personal data: erasure nulls it (and the other PII columns) but keeps
-- the row, its status, and the consent history so suppression still works.
CREATE TABLE newsletter_lists (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 64),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    is_default boolean NOT NULL DEFAULT false,
    position integer NOT NULL DEFAULT 0,
    archived_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Singleton row with the non-secret sending settings. The provider and its secrets live in env.
CREATE TABLE newsletter_provider_config (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    from_name text NOT NULL DEFAULT '',
    from_email text NOT NULL DEFAULT '',
    reply_to text NOT NULL DEFAULT '',
    postal_address text NOT NULL DEFAULT '',
    confirm_ttl_seconds integer NOT NULL DEFAULT 172800 CHECK (confirm_ttl_seconds BETWEEN 3600 AND 604800),
    double_optin_required boolean NOT NULL DEFAULT true,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE newsletter_subscribers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    email text,
    normalized_email text,
    display_name text,
    user_id bigint REFERENCES users(id) ON DELETE SET NULL,
    status text NOT NULL
        CHECK (status IN ('pending_confirm', 'active', 'unsubscribed', 'bounced', 'complained', 'erased')),
    format text NOT NULL DEFAULT 'html' CHECK (format IN ('html', 'plaintext')),
    source text NOT NULL CHECK (source IN ('public', 'account', 'admin')),
    ip_hash text,
    user_agent text,
    confirm_sends integer NOT NULL DEFAULT 0,
    confirm_window_started_at timestamptz,
    opted_in_at timestamptz,
    unsubscribed_at timestamptz,
    bounced_at timestamptz,
    complained_at timestamptz,
    erased_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK ((status = 'erased') = (normalized_email IS NULL)),
    CHECK ((email IS NULL) = (normalized_email IS NULL))
);
CREATE UNIQUE INDEX newsletter_subscribers_email_key ON newsletter_subscribers (normalized_email)
    WHERE normalized_email IS NOT NULL;
CREATE UNIQUE INDEX newsletter_subscribers_user_key ON newsletter_subscribers (user_id)
    WHERE user_id IS NOT NULL;
CREATE INDEX newsletter_subscribers_status_idx ON newsletter_subscribers (status, id);

-- state: pending waits for a confirmation click, active receives issues, left opted out.
CREATE TABLE newsletter_list_memberships (
    subscriber_id bigint NOT NULL REFERENCES newsletter_subscribers(id) ON DELETE CASCADE,
    list_id bigint NOT NULL REFERENCES newsletter_lists(id) ON DELETE CASCADE,
    state text NOT NULL CHECK (state IN ('pending', 'active', 'left')),
    joined_at timestamptz,
    left_at timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (subscriber_id, list_id)
);
CREATE INDEX newsletter_list_memberships_list_idx ON newsletter_list_memberships (list_id, state);

-- Single-purpose opaque tokens. Only the SHA-256 of the raw token is stored.
CREATE TABLE newsletter_tokens (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subscriber_id bigint NOT NULL REFERENCES newsletter_subscribers(id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('confirm', 'unsubscribe', 'preferences')),
    token_hash text NOT NULL UNIQUE CHECK (char_length(token_hash) = 64),
    issue_id bigint,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL
);
CREATE INDEX newsletter_tokens_subscriber_idx ON newsletter_tokens (subscriber_id, purpose);
CREATE INDEX newsletter_tokens_expiry_idx ON newsletter_tokens (expires_at);

-- Append-only consent history; erasure clears only feedback and ip_hash.
CREATE TABLE newsletter_consent_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subscriber_id bigint NOT NULL REFERENCES newsletter_subscribers(id) ON DELETE CASCADE,
    event text NOT NULL,
    list_slug text,
    source text NOT NULL,
    reason_code text,
    feedback text CHECK (char_length(feedback) <= 1000),
    ip_hash text,
    occurred_at timestamptz NOT NULL
);
CREATE INDEX newsletter_consent_audit_subscriber_idx ON newsletter_consent_audit (subscriber_id, occurred_at DESC);

CREATE TABLE newsletter_issues (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    subject text NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
    preheader text NOT NULL DEFAULT '' CHECK (char_length(preheader) <= 200),
    body_markdown text NOT NULL CHECK (char_length(body_markdown) BETWEEN 1 AND 200000),
    status text NOT NULL
        CHECK (status IN ('draft', 'scheduled', 'queued', 'sending', 'sent', 'cancelled')),
    send_at timestamptz,
    queued_at timestamptz,
    started_at timestamptz,
    completed_at timestamptz,
    sent_count integer NOT NULL DEFAULT 0,
    failed_count integer NOT NULL DEFAULT 0,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (status <> 'scheduled' OR send_at IS NOT NULL)
);
CREATE INDEX newsletter_issues_status_idx ON newsletter_issues (status, created_at DESC);
CREATE INDEX newsletter_issues_due_idx ON newsletter_issues (send_at) WHERE status = 'scheduled';

ALTER TABLE newsletter_tokens
    ADD CONSTRAINT newsletter_tokens_issue_fk FOREIGN KEY (issue_id) REFERENCES newsletter_issues(id) ON DELETE CASCADE;

CREATE TABLE newsletter_issue_lists (
    issue_id bigint NOT NULL REFERENCES newsletter_issues(id) ON DELETE CASCADE,
    list_id bigint NOT NULL REFERENCES newsletter_lists(id) ON DELETE RESTRICT,
    PRIMARY KEY (issue_id, list_id)
);

-- One row per issue and recipient. sending rows are claims; a stale claim is reclaimed.
CREATE TABLE newsletter_deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    issue_id bigint NOT NULL REFERENCES newsletter_issues(id) ON DELETE CASCADE,
    subscriber_id bigint NOT NULL REFERENCES newsletter_subscribers(id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('sending', 'sent', 'failed')),
    attempts integer NOT NULL DEFAULT 1,
    permanent boolean NOT NULL DEFAULT false,
    last_error text,
    sent_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (issue_id, subscriber_id)
);
CREATE INDEX newsletter_deliveries_issue_status_idx ON newsletter_deliveries (issue_id, status);

-- +goose Down
DROP TABLE IF EXISTS newsletter_deliveries;
DROP TABLE IF EXISTS newsletter_issue_lists;
DROP TABLE IF EXISTS newsletter_tokens;
DROP TABLE IF EXISTS newsletter_issues;
DROP TABLE IF EXISTS newsletter_consent_audit;
DROP TABLE IF EXISTS newsletter_list_memberships;
DROP TABLE IF EXISTS newsletter_subscribers;
DROP TABLE IF EXISTS newsletter_provider_config;
DROP TABLE IF EXISTS newsletter_lists;
