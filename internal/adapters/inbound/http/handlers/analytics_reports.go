package handlers

import (
	"context"
	"errors"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

type analyticsReportsAPI interface {
	Overview(ctx context.Context, q analyticsdomain.ReportQuery) (analyticsservice.Overview, error)
	Pages(ctx context.Context, q analyticsdomain.ReportQuery) (analyticsservice.PagesReport, error)
	Navigation(ctx context.Context, q analyticsdomain.ReportQuery) (analyticsservice.NavigationReport, error)
	Retention(ctx context.Context, q analyticsdomain.ReportQuery) (analyticsservice.RetentionReport, error)
	Search(ctx context.Context, q analyticsdomain.ReportQuery) (analyticsservice.SearchReport, error)
}

// adminAnalyticsOverviewHandler godoc
//
//	@Summary		Analytics overview
//	@Description	Totals, a series per period, the top 10 traffic sources, and audience (country, device, browser) for whole periods of the site time zone. Visitors are distinct within a period; a range total is the sum of per-period uniques, named visitor_days, visitor_weeks, or visitor_months after the grain, and plain visitors appears only when the window is a single period. previous holds the same number of periods just before the window (null with compare=none). Rates are null when their denominator is zero.
//	@Tags			admin
//	@Produce		json
//	@Param			from	query		string	false	"first day, YYYY-MM-DD (default 29 days before to)"
//	@Param			to		query		string	false	"last day, YYYY-MM-DD (default today)"
//	@Param			grain	query		string	false	"period length (default by range: up to 92 days day, up to 366 week, else month)"	Enums(day, week, month)
//	@Param			compare	query		string	false	"compare with the previous window"													Enums(previous, none)	default(previous)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/overview [get]
func adminAnalyticsOverviewHandler(reports analyticsReportsAPI) gin.HandlerFunc {
	return reportHandler(reports.Overview, responses.AnalyticsOverview)
}

// adminAnalyticsPagesHandler godoc
//
//	@Summary		Analytics pages
//	@Description	Top pages by views, average focus time (sort=time), or views gained over the previous window (sort=rising). previous_views and change appear with compare=previous or sort=rising. Pages outside the top 1000 of a period are folded into (other), which only sort=views lists.
//	@Tags			admin
//	@Produce		json
//	@Param			from	query		string	false	"first day, YYYY-MM-DD"
//	@Param			to		query		string	false	"last day, YYYY-MM-DD"
//	@Param			grain	query		string	false	"period length"	Enums(day, week, month)
//	@Param			compare	query		string	false	"compare"		Enums(previous, none)		default(previous)
//	@Param			sort	query		string	false	"order"			Enums(views, time, rising)	default(views)
//	@Param			limit	query		int		false	"rows"			minimum(1)					maximum(100)	default(20)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/pages [get]
func adminAnalyticsPagesHandler(reports analyticsReportsAPI) gin.HandlerFunc {
	return reportHandler(reports.Pages, responses.AnalyticsPages)
}

// adminAnalyticsNavigationHandler godoc
//
//	@Summary		Analytics navigation
//	@Description	The most common steps between pages (with their transition type) and the top entry and exit pages.
//	@Tags			admin
//	@Produce		json
//	@Param			from	query		string	false	"first day, YYYY-MM-DD"
//	@Param			to		query		string	false	"last day, YYYY-MM-DD"
//	@Param			grain	query		string	false	"period length"	Enums(day, week, month)
//	@Param			limit	query		int		false	"rows"			minimum(1)	maximum(100)	default(20)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/navigation [get]
func adminAnalyticsNavigationHandler(reports analyticsReportsAPI) gin.HandlerFunc {
	return reportHandler(reports.Navigation, responses.AnalyticsNavigation)
}

// adminAnalyticsRetentionHandler godoc
//
//	@Summary		Analytics retention
//	@Description	Daily cohorts of consented visitors first seen on each day of the window, with how many returned on day 1, 7, and 30 (null until that day has passed), weighted return rates over complete cohorts, and the new versus returning split of consented visitors.
//	@Tags			admin
//	@Produce		json
//	@Param			from	query		string	false	"first day, YYYY-MM-DD"
//	@Param			to		query		string	false	"last day, YYYY-MM-DD"
//	@Param			grain	query		string	false	"period length"	Enums(day, week, month)
//	@Param			compare	query		string	false	"compare"		Enums(previous, none)	default(previous)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/retention [get]
func adminAnalyticsRetentionHandler(reports analyticsReportsAPI) gin.HandlerFunc {
	return reportHandler(reports.Retention, responses.AnalyticsRetention)
}

// adminAnalyticsSearchHandler godoc
//
//	@Summary		Analytics search
//	@Description	Search totals and series, click-through rate, average seconds to the first click, top and zero-result queries, clicks per result position, and the most clicked results. Needs analytics.search.read.
//	@Tags			admin
//	@Produce		json
//	@Param			from	query		string	false	"first day, YYYY-MM-DD"
//	@Param			to		query		string	false	"last day, YYYY-MM-DD"
//	@Param			grain	query		string	false	"period length"	Enums(day, week, month)
//	@Param			compare	query		string	false	"compare"		Enums(previous, none)	default(previous)
//	@Param			limit	query		int		false	"rows"			minimum(1)				maximum(100)	default(20)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/search [get]
func adminAnalyticsSearchHandler(reports analyticsReportsAPI) gin.HandlerFunc {
	return reportHandler(reports.Search, responses.AnalyticsSearch)
}

func reportHandler[T any](
	run func(context.Context, analyticsdomain.ReportQuery) (T, error), render func(T) gin.H,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.AnalyticsReport
		if err := c.ShouldBindQuery(&req); err != nil {
			requests.FailValidation(c, err)
			return
		}

		report, err := run(c.Request.Context(), req.Query())

		switch {
		case err == nil:
			c.Header("Cache-Control", "private, no-store")
			responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAnalytics, responses.CaseSuccess, render(report))
		case errors.Is(err, analyticsdomain.ErrValidation):
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
		default:
			responses.Internal(c, err, "Failed to load the analytics report")
		}
	}
}

func analyticsReportControllers(deps Deps, a *routes.Analytics) {
	reports := deps.AnalyticsReports
	if reports == nil {
		return
	}

	a.AdminOverview = gate(deps, analyticsdomain.PermRead, editorRoles, adminAnalyticsOverviewHandler(reports))
	a.AdminPages = gate(deps, analyticsdomain.PermRead, editorRoles, adminAnalyticsPagesHandler(reports))
	a.AdminNavigation = gate(deps, analyticsdomain.PermRead, editorRoles, adminAnalyticsNavigationHandler(reports))
	a.AdminRetention = gate(deps, analyticsdomain.PermRead, editorRoles, adminAnalyticsRetentionHandler(reports))
	a.AdminSearch = gate(deps, analyticsdomain.PermSearchRead, editorRoles, adminAnalyticsSearchHandler(reports))
}
