package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterCommonRouter mounts general-purpose routes that are safe to expose
// without a version prefix — currently the liveness, readiness, and build-version
// probes described in docs/backend/router.md.
func RegisterCommonRouter(router *gin.RouterGroup, c Controllers) {
	get(router, "/health/live", "health.live", GroupHealth, AuthNone, c, c.Health.Live)
	get(router, "/health/ready", "health.ready", GroupHealth, AuthNone, c, c.Health.Ready)
	get(router, "/health/version", "health.version", GroupHealth, AuthNone, c, c.Health.Version)
}
