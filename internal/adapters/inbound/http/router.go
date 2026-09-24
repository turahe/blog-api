// Package http assembles the Gin router from core services and route registrations.
package http

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

// Dependencies are the services the HTTP adapter needs to build the router.
type Dependencies struct {
	Logger         *slog.Logger
	Health         healthports.Service
	Auth           authports.Service
	Users          *userservice.UserService
	AdminUsers     *authservice.AuthService
	TwoFactor      *authservice.AuthService
	AdminLogin     *authservice.AuthService
	RoleAdmin      *rbacservice.RoleService
	Profiles       *userservice.ProfileService
	EmailChange    authports.EmailChanger
	AvatarMaxBytes int64 // > 0 enables avatar routes
	Roles          handlers.RoleLookup
	RBAC           rbacports.Enforcer
	Posts          *postservice.PostService
	Categories     *categoryservice.CategoryService
	Tags           *tagservice.Service
	Media          mediaports.Service
	Comments       *commentservice.Service
	RateLimiter    middleware.Limiter
	CommentRates   handlers.CommentRates
	LoginPerMinute int
	Metrics        middleware.MetricsRecorder // nil disables request metrics
	Version        string
	TrustedProxies []string
	SwaggerEnabled bool
	// CacheBypassHeader honours `Cache-Control: no-cache` on public reads (debugging only).
	CacheBypassHeader bool
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

	global := gin.HandlersChain{
		middleware.RequestID(),
		middleware.Tracing(),
		middleware.SecurityHeaders(),
		middleware.AccessLog(deps.Logger),
	}
	if deps.Metrics != nil {
		global = append(global, middleware.Metrics(deps.Metrics))
	}

	global = append(global, middleware.Recovery(deps.Logger))
	if deps.CacheBypassHeader {
		global = append(global, middleware.CacheBypass())
	}

	controllerDeps := handlers.Deps{
		Logger:         deps.Logger,
		Health:         deps.Health,
		Auth:           deps.Auth,
		Users:          deps.Users,
		EmailChange:    deps.EmailChange,
		AvatarMaxBytes: deps.AvatarMaxBytes,
		Roles:          deps.Roles,
		RBAC:           deps.RBAC,
		Posts:          deps.Posts,
		Categories:     deps.Categories,
		Tags:           deps.Tags,
		Media:          deps.Media,
		Comments:       deps.Comments,
		RateLimiter:    deps.RateLimiter,
		CommentRates:   deps.CommentRates,
		LoginPerMinute: deps.LoginPerMinute,
		Version:        deps.Version,
	}
	if deps.Profiles != nil { // keep a nil service a nil interface
		controllerDeps.Profiles = deps.Profiles
	}

	if deps.AdminUsers != nil {
		controllerDeps.AdminUsers = deps.AdminUsers
	}

	if deps.TwoFactor != nil {
		controllerDeps.TwoFactor = deps.TwoFactor
	}

	if deps.AdminLogin != nil {
		controllerDeps.AdminLogin = deps.AdminLogin
	}

	if deps.RoleAdmin != nil {
		controllerDeps.RoleAdmin = deps.RoleAdmin
	}

	return routes.NewRouter(routes.Dependencies{
		Logger:           deps.Logger,
		TrustedProxies:   deps.TrustedProxies,
		GlobalMiddleware: global,
		Controllers:      handlers.NewControllers(controllerDeps),
		Auth: routes.AuthMiddleware{
			Optional: optional,
			Required: required,
		},
		HealthAlias:  healthAlias,
		MountSwagger: mountSwagger,
	})
}
