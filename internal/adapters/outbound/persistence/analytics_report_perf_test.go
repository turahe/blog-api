package persistence

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

const (
	reportBudget = 300 * time.Millisecond
	perfRuns     = 3
	perfDays     = analyticsdomain.MaxReportDays
)

// perfFirstDay is a Monday far from the dates other rollup tests use.
var perfFirstDay = time.Date(2011, 1, 3, 0, 0, 0, 0, time.UTC)

// perfPeriods lists every day, ISO week, and month period of the seeded window; it takes the
// first day, last day, first Monday, last day, first of month, and last day.
const perfPeriods = `SELECT 'day' AS grain, d::date AS p FROM generate_series(CAST(? AS date), CAST(? AS date), interval '1 day') d
UNION ALL SELECT 'week', w::date FROM generate_series(CAST(? AS date), CAST(? AS date), interval '1 week') w
UNION ALL SELECT 'month', m::date FROM generate_series(CAST(? AS date), CAST(? AS date), interval '1 month') m`

// perfSeeds fill each rollup table with its per-period cap: RollupTopN rows for paths,
// transitions, and queries (drawn from 5000 distinct values), fewer for the small tables.
var perfSeeds = []string{
	`INSERT INTO analytics_rollup_site SELECT grain, p, 5000 + (random()*500)::bigint, 2000, 2500, 900, 90000, 3000,
		400, 40, 200, 260, 800, 300, now() FROM periods`,
	`INSERT INTO analytics_rollup_pages SELECT grain, p, '/posts/' || ((g*7 + (p - CAST(? AS date))) % 5000),
		1 + (random()*400)::bigint, 1 + (random()*200)::bigint, (random()*50)::bigint, (random()*50)::bigint,
		(random()*9000)::bigint, (random()*300)::bigint FROM periods, generate_series(0, 999) g`,
	`INSERT INTO analytics_rollup_navigation SELECT grain, p, '/posts/' || ((g*7 + (p - CAST(? AS date))) % 5000),
		'/posts/' || ((g*11 + 1) % 5000), 'internal', 1 + (random()*100)::bigint FROM periods, generate_series(0, 999) g`,
	`INSERT INTO analytics_rollup_searches SELECT grain, p, 'query ' || ((g*7 + (p - CAST(? AS date))) % 5000),
		1 + (random()*50)::bigint, 1 + (random()*30)::bigint, (random()*5)::bigint, (random()*20)::bigint,
		(random()*25)::bigint, (random()*400)::bigint FROM periods, generate_series(0, 999) g`,
	`INSERT INTO analytics_rollup_referrers SELECT grain, p, 'site' || g || '.example', 1 + (random()*300)::bigint,
		1 + (random()*200)::bigint FROM periods, generate_series(0, 199) g`,
	`INSERT INTO analytics_rollup_dimensions SELECT grain, p, dim, dim || g, 1 + (random()*900)::bigint,
		1 + (random()*400)::bigint FROM periods, unnest(ARRAY['country', 'device', 'browser']) dim, generate_series(0, 29) g`,
	`INSERT INTO analytics_rollup_search_positions SELECT grain, p, g, 1 + (random()*80)::bigint
		FROM periods, generate_series(1, 20) g`,
	`INSERT INTO analytics_rollup_search_results SELECT grain, p, 'query ' || (g % 100), 'post',
		md5(grain || p || g)::uuid, 1 + (random()*40)::bigint FROM periods, generate_series(0, 199) g`,
	`INSERT INTO analytics_rollup_cohorts SELECT p, 300, 60, 30, 10, now() FROM periods WHERE grain = 'day'`,
}

var perfTables = []string{
	"analytics_rollup_site", "analytics_rollup_pages", "analytics_rollup_navigation", "analytics_rollup_searches",
	"analytics_rollup_referrers", "analytics_rollup_dimensions", "analytics_rollup_search_positions",
	"analytics_rollup_search_results", "analytics_rollup_cohorts",
}

// TestAnalyticsReportsStayFastOnTwoYearsOfRollups seeds two years of rollups at the
// per-period caps and checks every dashboard report answers within reportBudget (median of
// perfRuns) for the ranges an editor can ask for. Opt-in with TEST_ANALYTICS_PERF=1; run
// without -race.
//
//nolint:paralleltest // timings are only meaningful without other tests competing
func TestAnalyticsReportsStayFastOnTwoYearsOfRollups(t *testing.T) {
	if os.Getenv("TEST_ANALYTICS_PERF") != "1" {
		t.Skip("TEST_ANALYTICS_PERF not set to 1; skipping analytics report performance test")
	}

	tx := integrationTx(t)
	seedPerfRollups(t, tx)

	reports := analyticsservice.NewReports(NewAnalyticsReportRepository(tx), fixedZone("UTC"), system.Clock{})
	last := perfFirstDay.AddDate(0, 0, perfDays-1)

	windows := []struct {
		name  string
		days  int
		grain analyticsdomain.Grain
	}{
		{"30d daily", 30, analyticsdomain.GrainDay},
		{"92d daily", analyticsdomain.MaxDailyReportDays, analyticsdomain.GrainDay},
		{"365d weekly", 365, analyticsdomain.GrainWeek},
		{"731d weekly", perfDays, analyticsdomain.GrainWeek},
		{"731d monthly", perfDays, analyticsdomain.GrainMonth},
	}

	type report struct {
		name string
		run  func(context.Context, analyticsdomain.ReportQuery) error
	}

	pages := func(sort string) func(context.Context, analyticsdomain.ReportQuery) error {
		return func(ctx context.Context, q analyticsdomain.ReportQuery) error {
			q.Sort = sort
			_, err := reports.Pages(ctx, q)

			return err
		}
	}

	all := []report{
		{"overview", func(ctx context.Context, q analyticsdomain.ReportQuery) error {
			_, err := reports.Overview(ctx, q)
			return err
		}},
		{"pages by views", pages(analyticsdomain.SortViews)},
		{"pages by time", pages(analyticsdomain.SortTime)},
		{"pages rising", pages(analyticsdomain.SortRising)},
		{"navigation", func(ctx context.Context, q analyticsdomain.ReportQuery) error {
			_, err := reports.Navigation(ctx, q)
			return err
		}},
		{"search", func(ctx context.Context, q analyticsdomain.ReportQuery) error {
			_, err := reports.Search(ctx, q)
			return err
		}},
		{"retention", func(ctx context.Context, q analyticsdomain.ReportQuery) error {
			_, err := reports.Retention(ctx, q)
			return err
		}},
	}

	for _, w := range windows {
		q := analyticsdomain.ReportQuery{
			From: last.AddDate(0, 0, 1-w.days), To: last, Grain: w.grain, Compare: true, Limit: analyticsdomain.MaxReportLimit,
		}

		for _, r := range all {
			took := medianRun(t, func() error { return r.run(t.Context(), q) })
			t.Logf("%-14s %-16s %s", w.name, r.name, took)
			assert.LessOrEqual(t, took, reportBudget, "%s over %s", r.name, w.name)
		}
	}
}

func seedPerfRollups(t *testing.T, tx *gorm.DB) {
	t.Helper()

	last := perfFirstDay.AddDate(0, 0, perfDays-1)
	day, lastDay := perfFirstDay.Format(time.DateOnly), last.Format(time.DateOnly)
	month := time.Date(perfFirstDay.Year(), perfFirstDay.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	periodArgs := []any{day, lastDay, day, lastDay, month, lastDay}

	for _, seed := range perfSeeds {
		args := slices.Clone(periodArgs)
		if strings.Contains(seed, "?") {
			args = append(args, day)
		}

		require.NoError(t, tx.Exec("WITH periods AS ("+perfPeriods+") "+seed, args...).Error, seed)
	}

	for _, table := range perfTables {
		require.NoError(t, tx.Exec("ANALYZE "+table).Error)
	}

	var pageRows int64
	require.NoError(t, tx.Raw("SELECT count(*) FROM analytics_rollup_pages WHERE grain = 'day' AND period_start BETWEEN ? AND ?",
		day, lastDay).Scan(&pageRows).Error)
	require.Equal(t, int64(perfDays*analyticsdomain.RollupTopN), pageRows)
}

// medianRun times fn perfRuns times after one warm-up run.
func medianRun(t *testing.T, fn func() error) time.Duration {
	t.Helper()

	require.NoError(t, fn())

	runs := make([]time.Duration, perfRuns)

	for i := range runs {
		began := time.Now()

		require.NoError(t, fn())

		runs[i] = time.Since(began)
	}

	slices.Sort(runs)

	return runs[len(runs)/2]
}
