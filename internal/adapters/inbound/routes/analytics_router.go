package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterAnalyticsRouter mounts consent, ingestion, and admin-report analytics
// routes. Consent accepts an optional bearer token; ingestion runs through the
// configured rate limit/gate; admin reports require auth.
func RegisterAnalyticsRouter(router *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAnalytics

	consent := router.Group("")
	consent.Use(auth.Optional...)
	get(consent, "/analytics/consent", "analytics.consent.get", g, AuthNone, c, c.Analytics.ConsentGet)
	post(consent, "/analytics/consent", "analytics.consent.store", g, AuthOptional, c, c.Analytics.ConsentStore)
	del(consent, "/analytics/consent/:param1", "analytics.consent.withdraw", g, AuthOptional, c, c.Analytics.ConsentWithdraw)

	ingest := router.Group("")
	ingest.Use(c.Analytics.IngestLimits...)

	if c.Analytics.IngestGate != nil {
		ingest.Use(c.Analytics.IngestGate)
	}

	n := AuthNone
	post(ingest, "/analytics/ingest/navigation", "analytics.ingest.navigation", g, n, c, c.Analytics.Navigation)
	post(ingest, "/analytics/ingest/page-view", "analytics.ingest.page_view", g, n, c, c.Analytics.PageView)
	post(ingest, "/analytics/ingest/search", "analytics.ingest.search", g, n, c, c.Analytics.Search)
	post(ingest, "/analytics/ingest/search-click", "analytics.ingest.search_click", g, n, c, c.Analytics.SearchClick)
	post(ingest, "/analytics/ingest/time-spent", "analytics.ingest.time_spent", g, n, c, c.Analytics.TimeSpent)

	registerAnalyticsReportRoutes(router, auth, c)
}

// registerAnalyticsReportRoutes mounts the admin dashboard reports (rollups,
// live stream, exports) as part of RegisterAnalyticsRouter.
func registerAnalyticsReportRoutes(router *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	ag, ar := GroupAdmin, AuthRequired
	admin := router.Group("/admin")
	admin.Use(auth.Required...)

	get(admin, "/analytics/navigation", "admin.analytics.navigation", ag, ar, c, c.Analytics.AdminNavigation)
	get(admin, "/analytics/overview", "admin.analytics.overview", ag, ar, c, c.Analytics.AdminOverview)
	get(admin, "/analytics/pages", "admin.analytics.pages", ag, ar, c, c.Analytics.AdminPages)
	get(admin, "/analytics/realtime/stream", "admin.analytics.realtime.stream", ag, ar, c, c.Analytics.AdminRealtime)
	get(admin, "/analytics/retention", "admin.analytics.retention", ag, ar, c, c.Analytics.AdminRetention)
	get(admin, "/analytics/search", "admin.analytics.search", ag, ar, c, c.Analytics.AdminSearch)
	post(admin, "/analytics/export", "admin.analytics.export", ag, ar, c, c.Exports.Create)
	get(admin, "/analytics/exports", "admin.analytics.exports.list", ag, ar, c, c.Exports.List)
	get(admin, "/analytics/exports/:param1", "admin.analytics.exports.get", ag, ar, c, c.Exports.Get)
}
