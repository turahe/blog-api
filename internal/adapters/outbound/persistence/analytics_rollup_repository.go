package persistence

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"gorm.io/gorm"
)

// AnalyticsRollupRepository builds analytics rollups from raw events with set-based SQL.
type AnalyticsRollupRepository struct {
	db *gorm.DB
}

// NewAnalyticsRollupRepository returns an AnalyticsRollupRepository over db.
func NewAnalyticsRollupRepository(db *gorm.DB) *AnalyticsRollupRepository {
	return &AnalyticsRollupRepository{db: db}
}

// rollupTables holds every table keyed by (grain, period_start).
var rollupTables = []string{
	"analytics_rollup_site", "analytics_rollup_pages", "analytics_rollup_referrers", "analytics_rollup_dimensions",
	"analytics_rollup_navigation", "analytics_rollup_searches", "analytics_rollup_search_positions",
	"analytics_rollup_search_results",
}

// EarliestEvent returns the oldest raw page view or search time.
func (r *AnalyticsRollupRepository) EarliestEvent(ctx context.Context) (time.Time, bool, error) {
	var earliest *time.Time

	err := conn(ctx, r.db).Raw(`SELECT least(
	(SELECT min(occurred_at) FROM analytics_page_views),
	(SELECT min(occurred_at) FROM analytics_searches))`).Scan(&earliest).Error
	if err != nil || earliest == nil {
		return time.Time{}, false, err
	}

	return *earliest, true, nil
}

// Timezone returns the time zone the rollups were built in.
func (r *AnalyticsRollupRepository) Timezone(ctx context.Context) (string, bool, error) {
	var zones []string
	if err := conn(ctx, r.db).Raw(`SELECT timezone FROM analytics_rollup_state WHERE id`).Scan(&zones).Error; err != nil {
		return "", false, err
	}

	if len(zones) == 0 {
		return "", false, nil
	}

	return zones[0], true, nil
}

// SetTimezone records the time zone the rollups are built in.
func (r *AnalyticsRollupRepository) SetTimezone(ctx context.Context, timezone string, now time.Time) error {
	return conn(ctx, r.db).Exec(`INSERT INTO analytics_rollup_state (id, timezone, updated_at) VALUES (true, ?, ?)
ON CONFLICT (id) DO UPDATE SET timezone = EXCLUDED.timezone, updated_at = EXCLUDED.updated_at`, timezone, now.UTC()).Error
}

// DeleteFrom removes every period and cohort starting on or after day (YYYY-MM-DD).
func (r *AnalyticsRollupRepository) DeleteFrom(ctx context.Context, day string) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		for _, table := range rollupTables {
			if err := tx.Exec("DELETE FROM "+table+" WHERE period_start >= ?::date", day).Error; err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}

		return tx.Exec(`DELETE FROM analytics_rollup_cohorts WHERE cohort_day >= ?::date`, day).Error
	})
}

// RefreshFirstSeen records the first page view of each consented subject seen in [from, to),
// keeping an earlier one. Subjects erased meanwhile are skipped.
func (r *AnalyticsRollupRepository) RefreshFirstSeen(ctx context.Context, from, to time.Time) error {
	return conn(ctx, r.db).Exec(`
INSERT INTO analytics_subject_first_seen (subject_uuid, first_seen_at)
SELECT p.subject_uuid, min(p.occurred_at) FROM analytics_page_views p
WHERE p.subject_uuid IS NOT NULL AND p.occurred_at >= ? AND p.occurred_at < ?
	AND EXISTS (SELECT 1 FROM consent_subjects s WHERE s.uuid = p.subject_uuid)
GROUP BY p.subject_uuid
ON CONFLICT (subject_uuid) DO UPDATE
SET first_seen_at = LEAST(analytics_subject_first_seen.first_seen_at, EXCLUDED.first_seen_at)`, from.UTC(), to.UTC()).Error
}

// RecomputePeriod replaces every rollup row of the period in one transaction. An advisory
// lock serialises concurrent recomputes of the same period (the scheduler and a backfill).
func (r *AnalyticsRollupRepository) RecomputePeriod(ctx context.Context, period domain.Period, topN int, now time.Time) error {
	params := map[string]any{
		"grain": string(period.Grain), "period": period.Day(), "from": period.From, "to": period.To,
		"n": topN, "now": now.UTC(), "other": domain.Other,
	}

	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		key := "analytics-rollup:" + string(period.Grain) + ":" + period.Day()
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error; err != nil {
			return err
		}

		for _, table := range rollupTables {
			err := tx.Exec("DELETE FROM "+table+" WHERE grain = ? AND period_start = ?::date", params["grain"], params["period"]).Error
			if err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
		}

		for i, query := range rollupQueries {
			if err := tx.Exec(query, params).Error; err != nil {
				return fmt.Errorf("fill %s: %w", rollupTables[i], err)
			}
		}

		return nil
	})
}

// RecomputeCohort replaces the cohort row. A return whose day has not ended stays NULL.
func (r *AnalyticsRollupRepository) RecomputeCohort(ctx context.Context, cohort domain.Cohort, now time.Time) error {
	params := map[string]any{
		"day": cohort.Day.Day(), "from": cohort.Day.From, "to": cohort.Day.To, "now": now.UTC(),
	}

	var returns strings.Builder

	for i, ret := range cohort.Returns {
		n := strconv.Itoa(i)
		params["c"+n], params["f"+n], params["t"+n] = ret.Complete, ret.Day.From, ret.Day.To
		returns.WriteString(`,
	CASE WHEN CAST(@c` + n + ` AS boolean) THEN (SELECT count(*) FROM cohort c WHERE EXISTS (
		SELECT 1 FROM analytics_page_views p
		WHERE p.subject_uuid = c.subject_uuid AND p.occurred_at >= @f` + n + ` AND p.occurred_at < @t` + n + `)) END`)
	}

	return conn(ctx, r.db).Exec(`
INSERT INTO analytics_rollup_cohorts (cohort_day, size, day1, day7, day30, computed_at)
WITH cohort AS (
	SELECT subject_uuid FROM analytics_subject_first_seen WHERE first_seen_at >= @from AND first_seen_at < @to
)
SELECT CAST(@day AS date), (SELECT count(*) FROM cohort)`+returns.String()+`, CAST(@now AS timestamptz)
ON CONFLICT (cohort_day) DO UPDATE SET size = EXCLUDED.size, day1 = EXCLUDED.day1, day7 = EXCLUDED.day7,
	day30 = EXCLUDED.day30, computed_at = EXCLUDED.computed_at`, params).Error
}

// rollupQueries fill rollupTables, in the same order, for the period bound to the named
// parameters @grain, @period, @from, and @to.
var rollupQueries = []string{
	// analytics_rollup_site
	`INSERT INTO analytics_rollup_site (grain, period_start, views, visitors, sessions, bounces, focus_seconds,
	focus_views, searches, zero_result_searches, searches_with_click, search_clicks, consented_visitors, new_visitors,
	computed_at)
WITH pv AS (
	SELECT visitor_hash, session_id, subject_uuid FROM analytics_page_views WHERE occurred_at >= @from AND occurred_at < @to
),
per_session AS (SELECT session_id, count(*) AS views FROM pv GROUP BY session_id),
ts AS (
	SELECT coalesce(sum(focus_seconds), 0) AS seconds, count(*) AS n
	FROM analytics_time_spent WHERE started_at >= @from AND started_at < @to
),
s AS (SELECT uuid, result_count FROM analytics_searches WHERE occurred_at >= @from AND occurred_at < @to),
c AS (SELECT search_uuid FROM analytics_search_clicks WHERE search_uuid IN (SELECT uuid FROM s)),
subjects AS (SELECT DISTINCT subject_uuid FROM pv WHERE subject_uuid IS NOT NULL)
SELECT CAST(@grain AS text), CAST(@period AS date),
	(SELECT count(*) FROM pv), (SELECT count(DISTINCT visitor_hash) FROM pv), (SELECT count(*) FROM per_session),
	(SELECT count(*) FROM per_session WHERE views = 1), ts.seconds, ts.n,
	(SELECT count(*) FROM s), (SELECT count(*) FROM s WHERE result_count = 0),
	(SELECT count(DISTINCT search_uuid) FROM c), (SELECT count(*) FROM c),
	(SELECT count(*) FROM subjects),
	(SELECT count(*) FROM subjects JOIN analytics_subject_first_seen f USING (subject_uuid)
		WHERE f.first_seen_at >= @from AND f.first_seen_at < @to),
	CAST(@now AS timestamptz)
FROM ts`,

	// analytics_rollup_pages
	`INSERT INTO analytics_rollup_pages (grain, period_start, path, views, visitors, entries, exits, focus_seconds, focus_views)
WITH pv AS (
	SELECT path, visitor_hash,
		row_number() OVER (PARTITION BY session_id ORDER BY occurred_at, id) AS first_rank,
		row_number() OVER (PARTITION BY session_id ORDER BY occurred_at DESC, id DESC) AS last_rank
	FROM analytics_page_views WHERE occurred_at >= @from AND occurred_at < @to
),
ranked AS (SELECT path, row_number() OVER (ORDER BY count(*) DESC, path) AS rank FROM pv GROUP BY path),
bucket AS (SELECT path, CASE WHEN rank <= @n THEN path ELSE @other END AS bucket FROM ranked),
views AS (
	SELECT b.bucket, count(*) AS views, count(DISTINCT pv.visitor_hash) AS visitors,
		count(*) FILTER (WHERE pv.first_rank = 1) AS entries, count(*) FILTER (WHERE pv.last_rank = 1) AS exits
	FROM pv JOIN bucket b USING (path) GROUP BY b.bucket
),
focus AS (
	SELECT coalesce(b.bucket, @other) AS bucket, sum(t.focus_seconds) AS seconds, count(*) AS n
	FROM analytics_time_spent t LEFT JOIN bucket b USING (path)
	WHERE t.started_at >= @from AND t.started_at < @to GROUP BY 1
)
SELECT CAST(@grain AS text), CAST(@period AS date), bucket, coalesce(v.views, 0), coalesce(v.visitors, 0), coalesce(v.entries, 0),
	coalesce(v.exits, 0), coalesce(f.seconds, 0), coalesce(f.n, 0)
FROM views v FULL JOIN focus f USING (bucket)`,

	// analytics_rollup_referrers
	`INSERT INTO analytics_rollup_referrers (grain, period_start, host, sessions, visitors)
WITH entry AS (
	SELECT DISTINCT ON (session_id) coalesce(substring(referrer FROM '^https{0,1}://([^/]+)'), '(direct)') AS host, visitor_hash
	FROM analytics_page_views WHERE occurred_at >= @from AND occurred_at < @to
	ORDER BY session_id, occurred_at, id
),
ranked AS (SELECT host, row_number() OVER (ORDER BY count(*) DESC, host) AS rank FROM entry GROUP BY host)
SELECT CAST(@grain AS text), CAST(@period AS date), CASE WHEN r.rank <= @n THEN e.host ELSE @other END, count(*),
	count(DISTINCT e.visitor_hash)
FROM entry e JOIN ranked r USING (host) GROUP BY 3`,

	// analytics_rollup_dimensions
	`INSERT INTO analytics_rollup_dimensions (grain, period_start, dimension, value, views, visitors)
WITH pv AS (
	SELECT visitor_hash, coalesce(country_code, '(unknown)') AS country, device_type, browser
	FROM analytics_page_views WHERE occurred_at >= @from AND occurred_at < @to
)
SELECT CAST(@grain AS text), CAST(@period AS date), 'country', country, count(*), count(DISTINCT visitor_hash) FROM pv GROUP BY country
UNION ALL
SELECT CAST(@grain AS text), CAST(@period AS date), 'device', device_type, count(*), count(DISTINCT visitor_hash) FROM pv GROUP BY device_type
UNION ALL
SELECT CAST(@grain AS text), CAST(@period AS date), 'browser', browser, count(*), count(DISTINCT visitor_hash) FROM pv GROUP BY browser`,

	// analytics_rollup_navigation
	`INSERT INTO analytics_rollup_navigation (grain, period_start, from_path, to_path, transition, transitions)
WITH pairs AS (
	SELECT coalesce(from_path, '(entrance)') AS from_path, to_path, transition_type, count(*) AS n
	FROM analytics_navigation WHERE occurred_at >= @from AND occurred_at < @to GROUP BY 1, 2, 3
),
ranked AS (
	SELECT *, row_number() OVER (ORDER BY n DESC, from_path, to_path, transition_type) AS rank FROM pairs
)
SELECT CAST(@grain AS text), CAST(@period AS date),
	CASE WHEN rank <= @n THEN from_path ELSE @other END, CASE WHEN rank <= @n THEN to_path ELSE @other END,
	CASE WHEN rank <= @n THEN transition_type ELSE @other END, sum(n)
FROM ranked GROUP BY 3, 4, 5`,

	// analytics_rollup_searches
	`INSERT INTO analytics_rollup_searches (grain, period_start, query, searches, visitors, zero_results,
	searches_with_click, clicks, click_seconds)
WITH s AS (
	SELECT uuid, query, visitor_hash, result_count, occurred_at
	FROM analytics_searches WHERE occurred_at >= @from AND occurred_at < @to
),
c AS (
	SELECT search_uuid, count(*) AS clicks, min(occurred_at) AS first_click
	FROM analytics_search_clicks WHERE search_uuid IN (SELECT uuid FROM s) GROUP BY search_uuid
),
ranked AS (SELECT query, row_number() OVER (ORDER BY count(*) DESC, query) AS rank FROM s GROUP BY query)
SELECT CAST(@grain AS text), CAST(@period AS date), CASE WHEN r.rank <= @n THEN s.query ELSE @other END,
	count(*), count(DISTINCT s.visitor_hash), count(*) FILTER (WHERE s.result_count = 0),
	count(c.search_uuid), coalesce(sum(c.clicks), 0),
	coalesce(sum(greatest(extract(epoch FROM c.first_click - s.occurred_at), 0)), 0)::bigint
FROM s JOIN ranked r USING (query) LEFT JOIN c ON c.search_uuid = s.uuid
GROUP BY 3`,

	// analytics_rollup_search_positions
	`INSERT INTO analytics_rollup_search_positions (grain, period_start, position, clicks)
SELECT CAST(@grain AS text), CAST(@period AS date), c.position, count(*)
FROM analytics_search_clicks c JOIN analytics_searches s ON s.uuid = c.search_uuid
WHERE s.occurred_at >= @from AND s.occurred_at < @to
GROUP BY c.position`,

	// analytics_rollup_search_results
	`INSERT INTO analytics_rollup_search_results (grain, period_start, query, resource_type, resource_uuid, clicks)
SELECT CAST(@grain AS text), CAST(@period AS date), s.query, c.resource_type, c.resource_uuid, count(*)
FROM analytics_search_clicks c JOIN analytics_searches s ON s.uuid = c.search_uuid
WHERE s.occurred_at >= @from AND s.occurred_at < @to
GROUP BY 3, 4, 5
ORDER BY count(*) DESC, 3, 4, 5
LIMIT @n`,
}
