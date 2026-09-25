package routes

import (
	"github.com/gin-gonic/gin"
)

// registerAnalytics binds consent routes and the consent-gated ingestion routes. Consent
// accepts an optional token so a signed-in visitor can link authenticated analytics.
func registerAnalytics(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAnalytics

	consent := v1.Group("")
	consent.Use(auth.Optional...)
	get(consent, "/analytics/consent", "analytics.consent.get", g, AuthNone, c, c.Analytics.ConsentGet)
	post(consent, "/analytics/consent", "analytics.consent.store", g, AuthOptional, c, c.Analytics.ConsentStore)
	del(consent, "/analytics/consent/:param1", "analytics.consent.withdraw", g, AuthOptional, c, c.Analytics.ConsentWithdraw)

	ingest := v1.Group("")
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
}
