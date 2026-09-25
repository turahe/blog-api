package handlers

import (
	"context"
	"fmt"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

type fakeAnalyticsReports struct {
	query  analyticsdomain.ReportQuery
	header analyticsservice.Header
	err    error
}

func (f *fakeAnalyticsReports) Overview(_ context.Context, q analyticsdomain.ReportQuery) (analyticsservice.Overview, error) {
	f.query = q
	totals := analyticsdomain.Totals{Periods: len(f.header.Window.Periods)}
	totals.Views, totals.Visitors, totals.Sessions, totals.Bounces = 40, 9, 10, 4

	return analyticsservice.Overview{
		Header: f.header,
		Series: []analyticsdomain.SiteRow{{Period: "2026-09-01", Views: 40, Visitors: 9, Sessions: 10}},
		Totals: totals, Previous: &analyticsdomain.Totals{Periods: totals.Periods},
		Referrers:  []analyticsdomain.ReferrerRow{{Host: "example.org", Sessions: 3, Visitors: 2}},
		Dimensions: []analyticsdomain.DimensionRow{{Dimension: "device", Value: "mobile", Views: 30, Visitors: 7}},
	}, f.err
}

func (f *fakeAnalyticsReports) Pages(_ context.Context, q analyticsdomain.ReportQuery) (analyticsservice.PagesReport, error) {
	f.query = q

	return analyticsservice.PagesReport{
		Header: f.header, Sort: analyticsdomain.SortRising,
		Pages: []analyticsdomain.PageRow{{Path: "/a", Views: 12, PreviousViews: 5, FocusSeconds: 60, FocusViews: 4}},
	}, f.err
}

func (f *fakeAnalyticsReports) Navigation(_ context.Context, q analyticsdomain.ReportQuery) (analyticsservice.NavigationReport, error) {
	f.query = q

	return analyticsservice.NavigationReport{Header: f.header}, f.err
}

func (f *fakeAnalyticsReports) Retention(_ context.Context, q analyticsdomain.ReportQuery) (analyticsservice.RetentionReport, error) {
	f.query = q
	one, three := int64(1), int64(3)

	return analyticsservice.RetentionReport{
		Header: f.header,
		Cohorts: []analyticsdomain.CohortRow{
			{Day: "2026-09-01", Size: 10, Day1: &three, Day7: &one},
			{Day: "2026-09-02", Size: 30, Day1: &three},
		},
	}, f.err
}

func (f *fakeAnalyticsReports) Search(_ context.Context, q analyticsdomain.ReportQuery) (analyticsservice.SearchReport, error) {
	f.query = q

	return analyticsservice.SearchReport{
		Header: f.header, ClickTime: analyticsdomain.QueryRow{ClickSeconds: 12, SearchesWithClick: 4},
		Positions: []analyticsdomain.PositionRow{{Position: 1, Clicks: 3}, {Position: 2, Clicks: 1}},
	}, f.err
}

func reportHeader(t *testing.T, grain analyticsdomain.Grain, from, to string, compare bool) analyticsservice.Header {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	first, err := time.ParseInLocation(time.DateOnly, from, loc)
	require.NoError(t, err)
	last, err := time.ParseInLocation(time.DateOnly, to, loc)
	require.NoError(t, err)

	window := analyticsdomain.Window{Grain: grain, Periods: analyticsdomain.PeriodsBetween(grain, first, last, loc)}

	return analyticsservice.Header{Timezone: "Asia/Jakarta", Window: window, Comparison: window, Compare: compare}
}

func TestAdminAnalyticsOverviewRendersLabelledVisitorSums(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainDay, "2026-09-01", "2026-09-07", true)}
	w, body := runProfile(t, adminAnalyticsOverviewHandler(reports), profileRequest{
		method: nethttp.MethodGet, target: "/?from=2026-09-01&to=2026-09-07&grain=day&compare=previous",
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))

	assert.Equal(t, "2026-09-01", reports.query.From.Format(time.DateOnly))
	assert.Equal(t, "2026-09-07", reports.query.To.Format(time.DateOnly))
	assert.True(t, reports.query.Compare)

	data := dataOf(body)
	assert.Equal(t, "Asia/Jakarta", data["timezone"])
	assert.Equal(t, map[string]any{"from": "2026-09-01", "to": "2026-09-07", "periods": float64(7)}, data["window"])
	assert.NotNil(t, data["comparison"])

	totals, _ := data["totals"].(map[string]any)
	assert.InDelta(t, 9, totals["visitor_days"], 0)
	assert.NotContains(t, totals, "visitors", "a multi-period range has no distinct visitor total")
	assert.InDelta(t, 0.4, totals["bounce_rate"], 1e-9)
	assert.Nil(t, totals["avg_time_seconds"], "no focus data")

	previous, _ := data["previous"].(map[string]any)
	assert.Nil(t, previous["bounce_rate"])

	audience, _ := data["audience"].(map[string]any)
	devices, _ := audience["device"].([]any)
	require.Len(t, devices, 1)
	assert.Empty(t, audience["country"])
}

func TestAdminAnalyticsSinglePeriodAddsDistinctVisitors(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainMonth, "2026-09-01", "2026-09-30", false)}
	w, body := runProfile(t, adminAnalyticsOverviewHandler(reports), profileRequest{method: nethttp.MethodGet, target: "/?compare=none"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.False(t, reports.query.Compare)

	data := dataOf(body)
	assert.Nil(t, data["comparison"])

	totals, _ := data["totals"].(map[string]any)
	assert.InDelta(t, 9, totals["visitor_months"], 0)
	assert.InDelta(t, 9, totals["visitors"], 0)
}

func TestAdminAnalyticsPagesShowChangeWhenRising(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainDay, "2026-09-01", "2026-09-02", false)}
	w, body := runProfile(t, adminAnalyticsPagesHandler(reports), profileRequest{
		method: nethttp.MethodGet, target: "/?sort=rising&limit=5",
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, 5, reports.query.Limit)
	assert.Equal(t, analyticsdomain.SortRising, reports.query.Sort)

	pages, _ := dataOf(body)["pages"].([]any)
	require.Len(t, pages, 1)

	page, _ := pages[0].(map[string]any)
	assert.InDelta(t, 7, page["change"], 0)
	assert.InDelta(t, 15, page["avg_time_seconds"], 0)
}

func TestAdminAnalyticsRetentionWeighsCompleteCohorts(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainDay, "2026-09-01", "2026-09-02", false)}
	w, body := runProfile(t, adminAnalyticsRetentionHandler(reports), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusOK, w.Code)

	rates, _ := dataOf(body)["rates"].(map[string]any)
	assert.InDelta(t, 6.0/40, rates["day1"], 1e-9)
	assert.InDelta(t, 0.1, rates["day7"], 1e-9, "only the first cohort has reached day 7")
	assert.Nil(t, rates["day30"])
}

func TestAdminAnalyticsSearchRendersClickTimeAndShares(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainDay, "2026-09-01", "2026-09-02", false)}
	w, body := runProfile(t, adminAnalyticsSearchHandler(reports), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusOK, w.Code)

	data := dataOf(body)
	totals, _ := data["totals"].(map[string]any)
	assert.InDelta(t, 3, totals["avg_seconds_to_click"], 0)

	positions, _ := data["positions"].([]any)
	require.Len(t, positions, 2)

	first, _ := positions[0].(map[string]any)
	assert.InDelta(t, 0.75, first["share"], 1e-9)
}

func TestAdminAnalyticsReportsRejectBadQueries(t *testing.T) {
	t.Parallel()

	reports := &fakeAnalyticsReports{header: reportHeader(t, analyticsdomain.GrainDay, "2026-09-01", "2026-09-02", false)}

	for _, target := range []string{"/?from=09-01-2026", "/?grain=hour", "/?compare=yes", "/?limit=101", "/?sort=random"} {
		w, body := runProfile(t, adminAnalyticsPagesHandler(reports), profileRequest{method: nethttp.MethodGet, target: target})
		require.Equal(t, nethttp.StatusBadRequest, w.Code, target)
		require.Equal(t, "validation_error", errorCode(body), target)
	}

	reports.err = fmt.Errorf("%w: from is after to", analyticsdomain.ErrValidation)
	w, body := runProfile(t, adminAnalyticsPagesHandler(reports), profileRequest{method: nethttp.MethodGet, target: "/"})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "validation_error", errorCode(body))
}

func analyticsReportOps() map[string]gatedOp {
	return map[string]gatedOp{
		"overview":   {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminOverview }, analyticsdomain.PermRead, editorRoles},
		"pages":      {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminPages }, analyticsdomain.PermRead, editorRoles},
		"navigation": {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminNavigation }, analyticsdomain.PermRead, editorRoles},
		"retention":  {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminRetention }, analyticsdomain.PermRead, editorRoles},
		"search":     {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminSearch }, analyticsdomain.PermSearchRead, editorRoles},
		"realtime":   {func(c routes.Controllers) gin.HandlerFunc { return c.Analytics.AdminRealtime }, analyticsdomain.PermRealtimeRead, editorRoles},
	}
}

func TestAdminAnalyticsReportsCheckTheirPermission(t *testing.T) {
	t.Parallel()

	for op, tc := range analyticsReportOps() {
		enforcer := &recordingEnforcer{}
		handler := tc.handler(NewControllers(Deps{AnalyticsReports: &fakeAnalyticsReports{}, RBAC: enforcer}))

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "rbac.forbidden", errorCode(body), op)
		require.Equal(t, []string{tc.permission}, enforcer.checked, op)

		w, _ = runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/"})
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, op)
	}
}

func TestAdminAnalyticsReportsFallBackToEditors(t *testing.T) {
	t.Parallel()

	for op, tc := range analyticsReportOps() {
		deps := Deps{AnalyticsReports: &fakeAnalyticsReports{}, Roles: fakeRoleLookup{names: []string{roleAuthor}}}
		handler := tc.handler(NewControllers(deps))

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, op)
		require.Equal(t, "forbidden", errorCode(body), op)
	}
}

func TestAdminAnalyticsReportsStayStubsWithoutTheService(t *testing.T) {
	t.Parallel()

	c := NewControllers(Deps{})
	assert.Nil(t, c.Analytics.AdminOverview)
	assert.Nil(t, c.Analytics.AdminSearch)
}
