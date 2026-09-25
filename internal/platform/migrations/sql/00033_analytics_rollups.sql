-- +goose Up
-- Rollups of raw analytics events for dashboard reads. grain is day, week (ISO, starting
-- Monday), or month; period_start is the first local day of the period in the time zone
-- recorded in analytics_rollup_state. Recomputing a period replaces all of its rows. Paths,
-- referrer hosts, transitions, and queries keep the top values of each period and fold the rest
-- into '(other)'.
CREATE TABLE analytics_rollup_site (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    views bigint NOT NULL,
    visitors bigint NOT NULL,
    sessions bigint NOT NULL,
    bounces bigint NOT NULL,
    focus_seconds bigint NOT NULL,
    focus_views bigint NOT NULL,
    searches bigint NOT NULL,
    zero_result_searches bigint NOT NULL,
    searches_with_click bigint NOT NULL,
    search_clicks bigint NOT NULL,
    consented_visitors bigint NOT NULL,
    new_visitors bigint NOT NULL,
    computed_at timestamptz NOT NULL,
    PRIMARY KEY (grain, period_start)
);

CREATE TABLE analytics_rollup_pages (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    path text NOT NULL,
    views bigint NOT NULL,
    visitors bigint NOT NULL,
    entries bigint NOT NULL,
    exits bigint NOT NULL,
    focus_seconds bigint NOT NULL,
    focus_views bigint NOT NULL,
    PRIMARY KEY (grain, period_start, path)
);

-- Traffic sources: the referrer host of each session's first page view ('(direct)' without one).
CREATE TABLE analytics_rollup_referrers (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    host text NOT NULL,
    sessions bigint NOT NULL,
    visitors bigint NOT NULL,
    PRIMARY KEY (grain, period_start, host)
);

CREATE TABLE analytics_rollup_dimensions (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    dimension text NOT NULL CHECK (dimension IN ('country', 'device', 'browser')),
    value text NOT NULL,
    views bigint NOT NULL,
    visitors bigint NOT NULL,
    PRIMARY KEY (grain, period_start, dimension, value)
);

CREATE TABLE analytics_rollup_navigation (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    from_path text NOT NULL,
    to_path text NOT NULL,
    transition text NOT NULL,
    transitions bigint NOT NULL,
    PRIMARY KEY (grain, period_start, from_path, to_path, transition)
);

-- Clicks count toward the period of their search. click_seconds sums the time from each
-- search to its first click.
CREATE TABLE analytics_rollup_searches (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    query text NOT NULL,
    searches bigint NOT NULL,
    visitors bigint NOT NULL,
    zero_results bigint NOT NULL,
    searches_with_click bigint NOT NULL,
    clicks bigint NOT NULL,
    click_seconds bigint NOT NULL,
    PRIMARY KEY (grain, period_start, query)
);

CREATE TABLE analytics_rollup_search_positions (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    position integer NOT NULL,
    clicks bigint NOT NULL,
    PRIMARY KEY (grain, period_start, position)
);

-- The most clicked results per query; the long tail is not kept.
CREATE TABLE analytics_rollup_search_results (
    grain text NOT NULL CHECK (grain IN ('day', 'week', 'month')),
    period_start date NOT NULL,
    query text NOT NULL,
    resource_type text NOT NULL,
    resource_uuid uuid NOT NULL,
    clicks bigint NOT NULL,
    PRIMARY KEY (grain, period_start, query, resource_type, resource_uuid)
);

-- Retention cohorts of consented visitors by first local day seen. dayN counts the cohort
-- members with a page view on day N; it is NULL until that day has ended.
CREATE TABLE analytics_rollup_cohorts (
    cohort_day date PRIMARY KEY,
    size bigint NOT NULL,
    day1 bigint,
    day7 bigint,
    day30 bigint,
    computed_at timestamptz NOT NULL
);

-- First page view of each consented subject, kept past raw-event retention so returning
-- visitors stay returning. Erased with the subject.
CREATE TABLE analytics_subject_first_seen (
    subject_uuid uuid PRIMARY KEY,
    first_seen_at timestamptz NOT NULL
);
CREATE INDEX analytics_subject_first_seen_at_idx ON analytics_subject_first_seen (first_seen_at);

-- The time zone the rollups were built in; a different site.timezone triggers a rebuild.
CREATE TABLE analytics_rollup_state (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    timezone text NOT NULL,
    updated_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS analytics_rollup_state;
DROP TABLE IF EXISTS analytics_subject_first_seen;
DROP TABLE IF EXISTS analytics_rollup_cohorts;
DROP TABLE IF EXISTS analytics_rollup_search_results;
DROP TABLE IF EXISTS analytics_rollup_search_positions;
DROP TABLE IF EXISTS analytics_rollup_searches;
DROP TABLE IF EXISTS analytics_rollup_navigation;
DROP TABLE IF EXISTS analytics_rollup_dimensions;
DROP TABLE IF EXISTS analytics_rollup_referrers;
DROP TABLE IF EXISTS analytics_rollup_pages;
DROP TABLE IF EXISTS analytics_rollup_site;
