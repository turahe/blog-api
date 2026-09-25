package routes

import (
	"github.com/gin-gonic/gin"
)

// registerContractStubs mounts OpenAPI operations that are not yet implemented.
func registerContractStubs(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	ag, ar := GroupAdmin, AuthRequired

	post(admin, "/analytics/export", "admin.analytics.export", ag, ar, c, nil)
}
