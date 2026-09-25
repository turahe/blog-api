package persistence

import (
	"context"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"gorm.io/gorm"
)

// AnalyticsReportRepository reads analytics rollups for the admin dashboard.
type AnalyticsReportRepository struct {
	db *gorm.DB
}

// NewAnalyticsReportRepository returns an AnalyticsReportRepository over db.
func NewAnalyticsReportRepository(db *gorm.DB) *AnalyticsReportRepository {
	return &AnalyticsReportRepository{db: db}
}

// inWindow filters a rollup table on a domain.Selection: bind grain, first, last.
const inWindow = `grain = ? AND period_start BETWEEN CAST(? AS date) AND CAST(? AS date)`

func selArgs(sel domain.Selection, extra ...any) []any {
	return append([]any{string(sel.Grain), sel.First, sel.Last}, extra...)
}

// SiteRows returns the site rollup rows of the selection, oldest first.
func (r *AnalyticsReportRepository) SiteRows(ctx context.Context, sel domain.Selection) ([]domain.SiteRow, error) {
	var rows []domain.SiteRow

	err := conn(ctx, r.db).Raw(`SELECT to_char(period_start, 'YYYY-MM-DD') AS period, views, visitors, sessions, bounces,
	focus_seconds, focus_views, searches, zero_result_searches, searches_with_click, search_clicks, consented_visitors,
	new_visitors
FROM analytics_rollup_site WHERE `+inWindow+` ORDER BY period_start`, selArgs(sel)...).Scan(&rows).Error

	return rows, err
}

// Referrers returns the top traffic sources.
func (r *AnalyticsReportRepository) Referrers(ctx context.Context, sel domain.Selection, limit int) ([]domain.ReferrerRow, error) {
	var rows []domain.ReferrerRow

	err := conn(ctx, r.db).Raw(`SELECT host, sum(sessions) AS sessions, sum(visitors) AS visitors
FROM analytics_rollup_referrers WHERE `+inWindow+`
GROUP BY host ORDER BY sum(sessions) DESC, host LIMIT ?`, selArgs(sel, limit)...).Scan(&rows).Error

	return rows, err
}

// Dimensions returns every country, device, and browser value.
func (r *AnalyticsReportRepository) Dimensions(ctx context.Context, sel domain.Selection) ([]domain.DimensionRow, error) {
	var rows []domain.DimensionRow

	err := conn(ctx, r.db).Raw(`SELECT dimension, value, sum(views) AS views, sum(visitors) AS visitors
FROM analytics_rollup_dimensions WHERE `+inWindow+`
GROUP BY dimension, value ORDER BY dimension, sum(views) DESC, value`, selArgs(sel)...).Scan(&rows).Error

	return rows, err
}

// pageOrders are the ORDER BY and extra filter of each page sort.
var pageOrders = map[string]struct{ where, order string }{
	domain.SortViews:  {where: "true", order: "c.views DESC, c.path"},
	domain.SortTime:   {where: "c.focus_views > 0", order: "c.focus_seconds::float8 / c.focus_views DESC, c.views DESC, c.path"},
	domain.SortRising: {where: "true", order: "c.views - coalesce(p.views, 0) DESC, c.views DESC, c.path"},
}

// Pages returns the top pages with their views in prev.
func (r *AnalyticsReportRepository) Pages(
	ctx context.Context, cur, prev domain.Selection, sort string, limit int,
) ([]domain.PageRow, error) {
	order, ok := pageOrders[sort]
	if !ok {
		order = pageOrders[domain.SortViews]
	}

	remainder := "true"
	if sort != domain.SortViews {
		remainder = "c.path <> '" + domain.Other + "'"
	}

	var rows []domain.PageRow

	err := conn(ctx, r.db).Raw(`
WITH c AS (
	SELECT path, sum(views) AS views, sum(visitors) AS visitors, sum(entries) AS entries, sum(exits) AS exits,
		sum(focus_seconds) AS focus_seconds, sum(focus_views) AS focus_views
	FROM analytics_rollup_pages WHERE `+inWindow+` GROUP BY path
),
p AS (SELECT path, sum(views) AS views FROM analytics_rollup_pages WHERE `+inWindow+` GROUP BY path)
SELECT c.path, c.views, c.visitors, c.entries, c.exits, c.focus_seconds, c.focus_views,
	coalesce(p.views, 0) AS previous_views
FROM c LEFT JOIN p USING (path)
WHERE `+order.where+` AND `+remainder+`
ORDER BY `+order.order+` LIMIT ?`,
		append(selArgs(cur), selArgs(prev, limit)...)...).Scan(&rows).Error

	return rows, err
}

// Transitions returns the most common navigation steps.
func (r *AnalyticsReportRepository) Transitions(ctx context.Context, sel domain.Selection, limit int) ([]domain.TransitionRow, error) {
	var rows []domain.TransitionRow

	err := conn(ctx, r.db).Raw(`SELECT from_path AS "from", to_path AS "to", transition, sum(transitions) AS count
FROM analytics_rollup_navigation WHERE `+inWindow+`
GROUP BY 1, 2, 3 ORDER BY 4 DESC, 1, 2, 3 LIMIT ?`, selArgs(sel, limit)...).Scan(&rows).Error

	return rows, err
}

// EntryExitPages returns the pages most sessions started on and ended on, from one scan.
func (r *AnalyticsReportRepository) EntryExitPages(
	ctx context.Context, sel domain.Selection, limit int,
) ([]domain.PathCount, []domain.PathCount, error) {
	var rows []struct {
		List string
		domain.PathCount
	}

	err := conn(ctx, r.db).Raw(`
WITH t AS (
	SELECT path, sum(entries) AS entries, sum(exits) AS exits
	FROM analytics_rollup_pages WHERE `+inWindow+` GROUP BY path
)
SELECT list, path, count FROM (
	(SELECT 'entry' AS list, row_number() OVER (ORDER BY entries DESC, path) AS rank, path, entries AS count
		FROM t WHERE entries > 0 ORDER BY rank LIMIT ?)
	UNION ALL
	(SELECT 'exit', row_number() OVER (ORDER BY exits DESC, path), path, exits
		FROM t WHERE exits > 0 ORDER BY 2 LIMIT ?)
) lists ORDER BY list, rank`, selArgs(sel, limit, limit)...).Scan(&rows).Error

	var entries, exits []domain.PathCount

	for _, row := range rows {
		if row.List == "entry" {
			entries = append(entries, row.PathCount)
		} else {
			exits = append(exits, row.PathCount)
		}
	}

	return entries, exits, err
}

// SearchQueries returns the query totals, the top queries by searches, and the top queries by
// zero-result searches, from one scan.
func (r *AnalyticsReportRepository) SearchQueries(ctx context.Context, sel domain.Selection, limit int) (domain.SearchQueries, error) {
	var rows []struct {
		List string
		domain.QueryRow
	}

	err := conn(ctx, r.db).Raw(`
WITH q AS (
	SELECT query, sum(searches) AS searches, sum(visitors) AS visitors, sum(zero_results) AS zero_results,
		sum(searches_with_click) AS searches_with_click, sum(clicks) AS clicks, sum(click_seconds) AS click_seconds
	FROM analytics_rollup_searches WHERE `+inWindow+` GROUP BY query
)
SELECT list, query, searches, visitors, zero_results, searches_with_click, clicks, click_seconds FROM (
	SELECT 'total' AS list, 0::bigint AS rank, '' AS query, coalesce(sum(searches), 0) AS searches,
		coalesce(sum(visitors), 0) AS visitors, coalesce(sum(zero_results), 0) AS zero_results,
		coalesce(sum(searches_with_click), 0) AS searches_with_click, coalesce(sum(clicks), 0) AS clicks,
		coalesce(sum(click_seconds), 0) AS click_seconds
	FROM q
	UNION ALL
	(SELECT 'top', row_number() OVER (ORDER BY searches DESC, query), q.* FROM q ORDER BY 2 LIMIT ?)
	UNION ALL
	(SELECT 'zero', row_number() OVER (ORDER BY zero_results DESC, query), q.* FROM q
		WHERE zero_results > 0 AND query <> ? ORDER BY 2 LIMIT ?)
) lists ORDER BY list, rank`, selArgs(sel, limit, domain.Other, limit)...).Scan(&rows).Error

	var out domain.SearchQueries

	for _, row := range rows {
		switch row.List {
		case "total":
			out.Totals = row.QueryRow
		case "top":
			out.Top = append(out.Top, row.QueryRow)
		default:
			out.ZeroResults = append(out.ZeroResults, row.QueryRow)
		}
	}

	return out, err
}

// Positions returns clicks per result position, top position first.
func (r *AnalyticsReportRepository) Positions(ctx context.Context, sel domain.Selection, limit int) ([]domain.PositionRow, error) {
	var rows []domain.PositionRow

	err := conn(ctx, r.db).Raw(`SELECT position, sum(clicks) AS clicks
FROM analytics_rollup_search_positions WHERE `+inWindow+`
GROUP BY position ORDER BY position LIMIT ?`, selArgs(sel, limit)...).Scan(&rows).Error

	return rows, err
}

// ClickedResults returns the most clicked results with their query.
func (r *AnalyticsReportRepository) ClickedResults(ctx context.Context, sel domain.Selection, limit int) ([]domain.ResultRow, error) {
	var rows []domain.ResultRow

	err := conn(ctx, r.db).Raw(`SELECT query, resource_type, resource_uuid::text AS resource_uuid, sum(clicks) AS clicks
FROM analytics_rollup_search_results WHERE `+inWindow+`
GROUP BY 1, 2, 3 ORDER BY 4 DESC, 1, 2, 3 LIMIT ?`, selArgs(sel, limit)...).Scan(&rows).Error

	return rows, err
}

// Cohorts returns the cohorts of the local days first through last.
func (r *AnalyticsReportRepository) Cohorts(ctx context.Context, first, last string) ([]domain.CohortRow, error) {
	var rows []domain.CohortRow

	err := conn(ctx, r.db).Raw(`SELECT to_char(cohort_day, 'YYYY-MM-DD') AS day, size, day1, day7, day30
FROM analytics_rollup_cohorts WHERE cohort_day BETWEEN CAST(? AS date) AND CAST(? AS date)
ORDER BY cohort_day`, first, last).Scan(&rows).Error

	return rows, err
}
