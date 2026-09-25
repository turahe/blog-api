package routes

import (
	"github.com/gin-gonic/gin"
)

// registerContractStubs mounts OpenAPI operations that are not yet implemented.
func registerContractStubs(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	ag, ar := GroupAdmin, AuthRequired

	get(admin, "/analytics/navigation", "admin.analytics.navigation", ag, ar, c, nil)
	get(admin, "/analytics/overview", "admin.analytics.overview", ag, ar, c, nil)
	get(admin, "/analytics/pages", "admin.analytics.pages", ag, ar, c, nil)
	get(admin, "/analytics/realtime/stream", "admin.analytics.realtime.stream", ag, ar, c, nil)
	get(admin, "/analytics/retention", "admin.analytics.retention", ag, ar, c, nil)
	get(admin, "/analytics/search", "admin.analytics.search", ag, ar, c, nil)
	post(admin, "/analytics/export", "admin.analytics.export", ag, ar, c, nil)
}
