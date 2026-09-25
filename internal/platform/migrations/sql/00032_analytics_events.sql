-- +goose Up
-- Raw analytics events, written in batches by the API's ingest buffer. No IP address or
-- user agent is stored. visitor_hash identifies a visitor for unique counts: for a consent
-- subject that granted analytics it is stable, otherwise it is keyed by the day and cannot
-- link visits across days. subject_uuid (no foreign key: inserts must not fail when a subject
-- is erased mid-flush) is set only for granted subjects so their events can be erased.
-- uuid is the client's dedupe key; retried events are ignored.
CREATE TABLE analytics_page_views (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    subject_uuid uuid,
    visitor_hash text NOT NULL CHECK (char_length(visitor_hash) = 64),
    session_id uuid NOT NULL,
    path text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 512),
    referrer text CHECK (char_length(referrer) <= 512),
    country_code text CHECK (country_code ~ '^[A-Z]{2}$'),
    device_type text NOT NULL CHECK (device_type IN ('desktop', 'tablet', 'mobile')),
    browser text NOT NULL CHECK (char_length(browser) BETWEEN 1 AND 16),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX analytics_page_views_occurred_idx ON analytics_page_views (occurred_at);
CREATE INDEX analytics_page_views_subject_idx ON analytics_page_views (subject_uuid) WHERE subject_uuid IS NOT NULL;

-- One row per page view (uuid is the page view's uuid); heartbeats raise focus_seconds and
-- last_seen_at.
CREATE TABLE analytics_time_spent (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    subject_uuid uuid,
    visitor_hash text NOT NULL CHECK (char_length(visitor_hash) = 64),
    session_id uuid NOT NULL,
    path text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 512),
    focus_seconds integer NOT NULL CHECK (focus_seconds BETWEEN 0 AND 14400),
    started_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);
CREATE INDEX analytics_time_spent_started_idx ON analytics_time_spent (started_at);
CREATE INDEX analytics_time_spent_subject_idx ON analytics_time_spent (subject_uuid) WHERE subject_uuid IS NOT NULL;

CREATE TABLE analytics_navigation (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    subject_uuid uuid,
    visitor_hash text NOT NULL CHECK (char_length(visitor_hash) = 64),
    session_id uuid NOT NULL,
    from_path text CHECK (char_length(from_path) BETWEEN 1 AND 512),
    to_path text NOT NULL CHECK (char_length(to_path) BETWEEN 1 AND 512),
    transition_type text NOT NULL CHECK (transition_type IN ('internal', 'external', 'back_forward', 'direct')),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX analytics_navigation_occurred_idx ON analytics_navigation (occurred_at);
CREATE INDEX analytics_navigation_subject_idx ON analytics_navigation (subject_uuid) WHERE subject_uuid IS NOT NULL;

CREATE TABLE analytics_searches (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    subject_uuid uuid,
    visitor_hash text NOT NULL CHECK (char_length(visitor_hash) = 64),
    session_id uuid NOT NULL,
    query text NOT NULL CHECK (char_length(query) BETWEEN 1 AND 200),
    result_count integer NOT NULL CHECK (result_count >= 0),
    filters jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL
);
CREATE INDEX analytics_searches_occurred_idx ON analytics_searches (occurred_at);
CREATE INDEX analytics_searches_subject_idx ON analytics_searches (subject_uuid) WHERE subject_uuid IS NOT NULL;

-- search_uuid has no foreign key: a click may be flushed before its search, and a click
-- whose search never arrived is ignored by reports.
CREATE TABLE analytics_search_clicks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    search_uuid uuid NOT NULL,
    subject_uuid uuid,
    visitor_hash text NOT NULL CHECK (char_length(visitor_hash) = 64),
    session_id uuid NOT NULL,
    position integer NOT NULL CHECK (position BETWEEN 1 AND 1000),
    resource_type text NOT NULL CHECK (resource_type IN ('post', 'page', 'category', 'tag')),
    resource_uuid uuid NOT NULL,
    occurred_at timestamptz NOT NULL
);
CREATE INDEX analytics_search_clicks_search_idx ON analytics_search_clicks (search_uuid);
CREATE INDEX analytics_search_clicks_occurred_idx ON analytics_search_clicks (occurred_at);
CREATE INDEX analytics_search_clicks_subject_idx ON analytics_search_clicks (subject_uuid) WHERE subject_uuid IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS analytics_search_clicks;
DROP TABLE IF EXISTS analytics_searches;
DROP TABLE IF EXISTS analytics_navigation;
DROP TABLE IF EXISTS analytics_time_spent;
DROP TABLE IF EXISTS analytics_page_views;
