package http

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// Dependencies are the services the HTTP adapter needs to build the router.
type Dependencies struct {
	Logger         *slog.Logger
	Health         healthports.Service
	Auth           authports.Service
	Users          *userservice.UserService
	Roles          handlers.RoleLookup
	RBAC           rbacports.Enforcer
	Posts          *postservice.PostService
	Categories     *categoryservice.CategoryService
	Tags           *tagservice.Service
	Media          mediaports.Service
	Version        string
	TrustedProxies []string
	SwaggerEnabled bool
}

// NewRouter wires middleware + controllers into routes.NewRouter.
func NewRouter(deps Dependencies) (*gin.Engine, error) {
	var optional, required gin.HandlersChain
	if deps.Auth != nil {
		required = gin.HandlersChain{middleware.BearerAuth(deps.Auth)}
		optional = gin.HandlersChain{middleware.OptionalBearerAuth(deps.Auth)}
	}

	var healthAlias gin.HandlerFunc
	if deps.Health != nil {
		healthAlias = handlers.Live(deps.Health)
	}

	var mountSwagger func(gin.IRoutes) error
	if deps.SwaggerEnabled {
		mountSwagger = swagger.Mount
	}

	return routes.NewRouter(routes.Dependencies{
		Logger:         deps.Logger,
		TrustedProxies: deps.TrustedProxies,
		GlobalMiddleware: gin.HandlersChain{
			middleware.RequestID(),
			middleware.SecurityHeaders(),
			middleware.AccessLog(deps.Logger),
			middleware.Recovery(deps.Logger),
		},
		Controllers: handlers.NewControllers(handlers.Deps{
			Health:     deps.Health,
			Auth:       deps.Auth,
			Users:      deps.Users,
			Roles:      deps.Roles,
			RBAC:       deps.RBAC,
			Posts:      deps.Posts,
			Categories: deps.Categories,
			Tags:       deps.Tags,
			Media:      deps.Media,
			Version:    deps.Version,
		}),
		Auth: routes.AuthMiddleware{
			Optional: optional,
			Required: required,
		},
		HealthAlias:  healthAlias,
		MountSwagger: mountSwagger,
	})
}
