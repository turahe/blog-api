package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

type fakeReportRepo struct {
	site      map[string][]domain.SiteRow
	siteCalls []domain.Selection
	pages     [2]domain.Selection
	sort      string
	limits    []int
	cohorts   [2]string
}

func (f *fakeReportRepo) SiteRows(_ context.Context, sel domain.Selection) ([]domain.SiteRow, error) {
	f.siteCalls = append(f.siteCalls, sel)

	return f.site[sel.First], nil
}

func (f *fakeReportRepo) Referrers(_ context.Context, _ domain.Selection, limit int) ([]domain.ReferrerRow, error) {
	f.limits = append(f.limits, limit)

	return nil, nil
}

func (f *fakeReportRepo) Dimensions(context.Context, domain.Selection) ([]domain.DimensionRow, error) {
	return nil, nil
}

func (f *fakeReportRepo) Pages(_ context.Context, cur, prev domain.Selection, sort string, limit int) ([]domain.PageRow, error) {
	f.pages, f.sort = [2]domain.Selection{cur, prev}, sort
	f.limits = append(f.limits, limit)

	return nil, nil
}

func (f *fakeReportRepo) Transitions(context.Context, domain.Selection, int) ([]domain.TransitionRow, error) {
	return nil, nil
}

func (f *fakeReportRepo) EntryExitPages(context.Context, domain.Selection, int) ([]domain.PathCount, []domain.PathCount, error) {
	return nil, nil, nil
}

func (f *fakeReportRepo) SearchQueries(context.Context, domain.Selection, int) (domain.SearchQueries, error) {
	return domain.SearchQueries{Totals: domain.QueryRow{ClickSeconds: 30, SearchesWithClick: 3}}, nil
}

func (f *fakeReportRepo) Positions(context.Context, domain.Selection, int) ([]domain.PositionRow, error) {
	return nil, nil
}

func (f *fakeReportRepo) ClickedResults(context.Context, domain.Selection, int) ([]domain.ResultRow, error) {
	return nil, nil
}

func (f *fakeReportRepo) Cohorts(_ context.Context, first, last string) ([]domain.CohortRow, error) {
	f.cohorts = [2]string{first, last}

	return nil, nil
}

type fixedTimezone string

func (z fixedTimezone) Timezone(context.Context) (string, error) { return string(z), nil }

func newTestReports(repo *fakeReportRepo) *Reports {
	// 2026-09-25 23:30 in Jakarta (UTC+7) is still 2026-09-25 16:30 UTC.
	return NewReports(repo, fixedTimezone("Asia/Jakarta"), &fixedClock{now: time.Date(2026, 9, 25, 16, 30, 0, 0, time.UTC)})
}

func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)

	return t
}

func TestReportsDefaultToTheLast30LocalDays(t *testing.T) {
	t.Parallel()

	repo := &fakeReportRepo{site: map[string][]domain.SiteRow{
		"2026-08-27": {
			{Period: "2026-08-27", Views: 5, Visitors: 2, Sessions: 3},
			{Period: "2026-09-25", Views: 7, Visitors: 4, Sessions: 4},
		},
		"2026-07-28": {{Period: "2026-08-01", Views: 11, Visitors: 6}},
	}}

	out, err := newTestReports(repo).Overview(context.Background(), domain.ReportQuery{Compare: true})
	require.NoError(t, err)

	assert.Equal(t, "Asia/Jakarta", out.Timezone)
	assert.Equal(t, domain.GrainDay, out.Window.Grain)
	assert.Equal(t, "2026-08-27", out.Window.FirstDay())
	assert.Equal(t, "2026-09-25", out.Window.LastDay())
	assert.Equal(t, "2026-07-28", out.Comparison.FirstDay())
	assert.Equal(t, "2026-08-26", out.Comparison.LastDay())

	require.Len(t, out.Series, 30)
	assert.Equal(t, "2026-08-28", out.Series[1].Period, "missing periods are zero-filled")
	assert.Zero(t, out.Series[1].Views)
	assert.Equal(t, int64(12), out.Totals.Views)
	assert.Equal(t, int64(6), out.Totals.Visitors, "visitor-days add up")
	assert.Equal(t, 30, out.Totals.Periods)

	require.NotNil(t, out.Previous)
	assert.Equal(t, int64(11), out.Previous.Views)
	assert.Equal(t, []int{overviewReferrers}, repo.limits)
}

func TestReportsWithoutComparisonSkipThePreviousWindow(t *testing.T) {
	t.Parallel()

	repo := &fakeReportRepo{}

	out, err := newTestReports(repo).Retention(context.Background(), domain.ReportQuery{
		From: date("2026-09-01"), To: date("2026-09-10"),
	})
	require.NoError(t, err)

	assert.Nil(t, out.Previous)
	assert.Len(t, repo.siteCalls, 1)
	assert.Equal(t, [2]string{"2026-09-01", "2026-09-10"}, repo.cohorts)
}

func TestReportsPickTheGrainFromTheRange(t *testing.T) {
	t.Parallel()

	repo := &fakeReportRepo{}

	out, err := newTestReports(repo).Pages(context.Background(), domain.ReportQuery{
		From: date("2026-01-01"), To: date("2026-06-30"), Sort: domain.SortRising, Limit: 5,
	})
	require.NoError(t, err)

	assert.Equal(t, domain.GrainWeek, out.Window.Grain)
	assert.Equal(t, "2025-12-29", out.Window.FirstDay(), "weeks start on Monday and cover whole periods")
	assert.Equal(t, "2026-07-05", out.Window.LastDay())
	assert.Len(t, out.Comparison.Periods, len(out.Window.Periods))
	assert.Equal(t, domain.SortRising, repo.sort)
	assert.Equal(t, out.Comparison.Selection(), repo.pages[1])
	assert.Equal(t, []int{5}, repo.limits)

	out, err = newTestReports(repo).Pages(context.Background(), domain.ReportQuery{
		From: date("2025-01-01"), To: date("2026-06-30"),
	})
	require.NoError(t, err)
	assert.Equal(t, domain.GrainMonth, out.Window.Grain)
	assert.Equal(t, domain.SortViews, out.Sort)
	assert.Equal(t, domain.DefaultReportLimit, repo.limits[1])
}

func TestReportsExplicitMonthGrainCoversWholeMonths(t *testing.T) {
	t.Parallel()

	out, err := newTestReports(&fakeReportRepo{}).Navigation(context.Background(), domain.ReportQuery{
		From: date("2026-02-10"), To: date("2026-03-05"), Grain: domain.GrainMonth,
	})
	require.NoError(t, err)

	assert.Equal(t, "2026-02-01", out.Window.FirstDay())
	assert.Equal(t, "2026-03-31", out.Window.LastDay())
	assert.Equal(t, "2025-12-01", out.Comparison.FirstDay())
	assert.Equal(t, "2026-01-31", out.Comparison.LastDay())
}

func TestReportsSearchSumsClickTimeOverAllQueries(t *testing.T) {
	t.Parallel()

	out, err := newTestReports(&fakeReportRepo{}).Search(context.Background(), domain.ReportQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(30), out.ClickTime.ClickSeconds)
}

func TestReportsRejectInvalidQueries(t *testing.T) {
	t.Parallel()

	cases := map[string]domain.ReportQuery{
		"from after to":  {From: date("2026-09-10"), To: date("2026-09-01")},
		"too long":       {From: date("2024-01-01"), To: date("2026-01-02")},
		"daily too long": {From: date("2026-01-01"), To: date("2026-04-03"), Grain: domain.GrainDay},
		"bad grain":      {Grain: "hour"},
		"limit":          {Limit: domain.MaxReportLimit + 1},
		"sort":           {Sort: "random"},
	}

	for name, q := range cases {
		_, err := newTestReports(&fakeReportRepo{}).Pages(context.Background(), q)
		require.ErrorIs(t, err, domain.ErrValidation, name)
	}

	_, err := newTestReports(&fakeReportRepo{}).Overview(context.Background(), domain.ReportQuery{
		From: date("2024-01-01"), To: date("2025-12-31"),
	})
	require.NoError(t, err, "731 days is the longest range")

	_, err = newTestReports(&fakeReportRepo{}).Overview(context.Background(), domain.ReportQuery{
		From: date("2026-01-01"), To: date("2026-04-03"), Grain: domain.GrainWeek,
	})
	require.NoError(t, err, "longer ranges read weekly or monthly rollups")

	_, err = newTestReports(&fakeReportRepo{}).Overview(context.Background(), domain.ReportQuery{
		From: date("2026-01-01"), To: date("2026-04-02"), Grain: domain.GrainDay,
	})
	require.NoError(t, err, "92 days is the longest daily range")
}
