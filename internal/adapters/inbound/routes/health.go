package routes

import (
	"github.com/gin-gonic/gin"
)

// registerHealth binds health probes on the engine root.
func registerHealth(router gin.IRoutes, c Controllers) {
	get(router, "/health/live", "health.live", GroupHealth, AuthNone, c, c.Health.Live)
	get(router, "/health/ready", "health.ready", GroupHealth, AuthNone, c, c.Health.Ready)
	get(router, "/health/version", "health.version", GroupHealth, AuthNone, c, c.Health.Version)
}
