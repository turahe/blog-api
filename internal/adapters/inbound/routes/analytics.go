package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterAnalytics binds analytics ingest/consent routes (stubs until implemented).
func RegisterAnalytics(v1 gin.IRoutes, c Controllers) {
	g := GroupAnalytics
	n := AuthNone
	get(v1, "/analytics/consent", "analytics.consent.get", g, n, c, nil)
	post(v1, "/analytics/consent", "analytics.consent.store", g, n, c, nil)
	del(v1, "/analytics/consent/:param1", "analytics.consent.withdraw", g, n, c, nil)
	post(v1, "/analytics/ingest/navigation", "analytics.ingest.navigation", g, n, c, nil)
	post(v1, "/analytics/ingest/page-view", "analytics.ingest.page_view", g, n, c, nil)
	post(v1, "/analytics/ingest/search", "analytics.ingest.search", g, n, c, nil)
	post(v1, "/analytics/ingest/search-click", "analytics.ingest.search_click", g, n, c, nil)
	post(v1, "/analytics/ingest/time-spent", "analytics.ingest.time_spent", g, n, c, nil)
}
