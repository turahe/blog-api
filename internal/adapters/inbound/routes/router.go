// Package routes owns route metadata and builds the Gin engine for the HTTP API.
package routes

import (
	"log/slog"
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// Dependencies are injected building blocks for NewRouter.
// GlobalMiddleware and Auth must be constructed by the caller (avoids import cycles
// with http/middleware and http/handlers).
type Dependencies struct {
	Logger           *slog.Logger
	TrustedProxies   []string
	GlobalMiddleware gin.HandlersChain
	Controllers      Controllers
	Auth             AuthMiddleware
	HealthAlias      gin.HandlerFunc // optional GET /api/v1/health
	MountSwagger     func(gin.IRoutes) error
}

// NewRouter builds the Gin engine with global middleware and Register*.
func NewRouter(deps Dependencies) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	if len(deps.GlobalMiddleware) > 0 {
		router.Use(deps.GlobalMiddleware...)
	}
	if err := router.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, err
	}

	Register(router, deps.Controllers, deps.Auth)

	if deps.HealthAlias != nil {
		router.GET("/api/v1/health", deps.HealthAlias)
	}
	if deps.MountSwagger != nil {
		if err := deps.MountSwagger(router); err != nil {
			return nil, err
		}
	}

	router.NoRoute(func(c *gin.Context) {
		responses.Failure(c, nethttp.StatusNotFound, "route.not_found", "Route not found")
	})
	router.NoMethod(func(c *gin.Context) {
		responses.Failure(c, nethttp.StatusMethodNotAllowed, "method.not_allowed", "Method not allowed")
	})
	return router, nil
}
