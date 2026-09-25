package persistence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

// seedReportRollups builds day rollups for rollupDay and the day after, and rollupDay's cohort.
func seedReportRollups(t *testing.T, tx *gorm.DB) {
	t.Helper()

	ctx := t.Context()
	seedRollupDay(t, tx)

	repo := NewAnalyticsRollupRepository(tx)
	require.NoError(t, repo.RefreshFirstSeen(ctx, rollupDay, rollupDay.AddDate(0, 0, 2)))

	for _, day := range []time.Time{rollupDay, rollupDay.AddDate(0, 0, 1)} {
		period := analyticsdomain.PeriodOf(analyticsdomain.GrainDay, day, time.UTC)
		require.NoError(t, repo.RecomputePeriod(ctx, period, analyticsdomain.RollupTopN, time.Now()))
	}

	require.NoError(t, repo.RecomputeCohort(ctx, analyticsdomain.CohortOf(rollupDay, time.UTC, time.Now()), time.Now()))
}

func reportQuery(sort string) analyticsdomain.ReportQuery {
	return analyticsdomain.ReportQuery{
		From: rollupDay, To: rollupDay.AddDate(0, 0, 1), Grain: analyticsdomain.GrainDay, Compare: true, Sort: sort,
	}
}

func TestAnalyticsReportsReadRollups(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	seedReportRollups(t, tx)

	reports := analyticsservice.NewReports(NewAnalyticsReportRepository(tx), fixedZone("UTC"), system.Clock{})

	overview, err := reports.Overview(ctx, reportQuery(""))
	require.NoError(t, err)
	require.Len(t, overview.Series, 2)
	assert.Equal(t, "2021-03-10", overview.Series[0].Period)
	assert.Equal(t, int64(3), overview.Series[0].Views)
	assert.Equal(t, int64(4), overview.Totals.Views)
	assert.Equal(t, int64(3), overview.Totals.Visitors, "two visitors on the first day, one on the second")
	require.NotNil(t, overview.Previous)
	assert.Zero(t, overview.Previous.Views)
	require.NotEmpty(t, overview.Referrers)
	assert.Contains(t, overview.Referrers, analyticsdomain.ReferrerRow{Host: "news.example", Sessions: 1, Visitors: 1})
	assert.NotEmpty(t, overview.Dimensions)

	pages, err := reports.Pages(ctx, reportQuery(analyticsdomain.SortRising))
	require.NoError(t, err)
	require.NotEmpty(t, pages.Pages)
	assert.Equal(t, "/a", pages.Pages[0].Path)
	assert.Equal(t, int64(3), pages.Pages[0].Views)
	assert.Zero(t, pages.Pages[0].PreviousViews)

	byTime, err := reports.Pages(ctx, reportQuery(analyticsdomain.SortTime))
	require.NoError(t, err)
	require.Len(t, byTime.Pages, 1, "only pages with focus time")
	assert.Equal(t, int64(30), byTime.Pages[0].FocusSeconds)

	nav, err := reports.Navigation(ctx, reportQuery(""))
	require.NoError(t, err)
	assert.Contains(t, nav.Transitions, analyticsdomain.TransitionRow{
		From: "/a", To: "/b", Transition: string(analyticsdomain.TransitionInternal), Count: 1,
	})
	assert.NotEmpty(t, nav.Entries)
	assert.NotEmpty(t, nav.Exits)

	search, err := reports.Search(ctx, reportQuery(""))
	require.NoError(t, err)
	assert.Equal(t, int64(2), search.Totals.Searches)
	assert.Equal(t, int64(10), search.ClickTime.ClickSeconds)
	assert.Equal(t, []analyticsdomain.PositionRow{{Position: 2, Clicks: 1}}, search.Positions)
	require.Len(t, search.ZeroResults, 1)
	assert.Equal(t, "nothing", search.ZeroResults[0].Query)
	require.Len(t, search.Results, 1)
	assert.Equal(t, "go", search.Results[0].Query)

	retention, err := reports.Retention(ctx, reportQuery(""))
	require.NoError(t, err)
	require.Len(t, retention.Cohorts, 1)
	assert.Equal(t, "2021-03-10", retention.Cohorts[0].Day)
	require.NotNil(t, retention.Cohorts[0].Day1)
	assert.Equal(t, int64(1), *retention.Cohorts[0].Day1)
}
