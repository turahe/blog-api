// Package http assembles the Gin router from core services and route registrations.
package http

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	auditports "github.com/turahe/blog-api/internal/core/audit/ports"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
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
	OAuth          *authservice.AuthService
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
	Settings       *settingsservice.Service // nil keeps the settings routes as 501 stubs
	Consent        *consentservice.Service  // nil keeps the consent routes as 501 stubs
	// AnalyticsIngest accepts telemetry; nil keeps the ingest routes as 501 stubs.
	AnalyticsIngest *analyticsservice.Ingest
	// AnalyticsReports answers the admin dashboard; nil keeps the report routes as 501 stubs.
	AnalyticsReports *analyticsservice.Reports
	// AnalyticsLive and AnalyticsLiveStreams feed the live stream; nil (no broker) answers 503.
	AnalyticsLive            *analyticsservice.Board
	AnalyticsLiveStreams     *realtime.Hub
	AnalyticsIngestPerMinute int
	AnalyticsCountryHeader   string
	// PrivacyRequests queues data exports and erasures; nil keeps the routes as 501 stubs.
	PrivacyRequests *privacyservice.Service
	// Impersonation serves /admin/impersonation and verifies impersonation tokens; nil keeps
	// the routes as 501 stubs and refuses impersonation tokens.
	Impersonation *impservice.Service
	// Newsletter serves the newsletter routes; nil keeps them as 501 stubs.
	Newsletter *nlservice.Service
	// NewsletterProvider describes the environment-configured provider for the admin config.
	NewsletterProvider responses.NewsletterProvider
	RateLimiter        middleware.Limiter
	CommentRates       handlers.CommentRates
	LoginPerMinute     int
	Metrics            middleware.MetricsRecorder // nil disables request metrics
	// MaxInFlight sheds requests beyond this many concurrent ones with 503; 0 disables.
	MaxInFlight   int
	Audit         auditports.Writer // nil disables audit logging
	Activity      *auditservice.Activity
	Notifications *notificationservice.Inbox // nil keeps the inbox routes as 501 stubs
	// NotificationStream fans live notices out to SSE clients; nil answers 503.
	NotificationStream *realtime.Hub
	SSEPingInterval    time.Duration
	Version            string
	TrustedProxies     []string
	SwaggerEnabled     bool
	// CacheBypassHeader honours `Cache-Control: no-cache` on public reads (debugging only).
	CacheBypassHeader bool
}

// NewRouter wires middleware + controllers into routes.NewRouter.
func NewRouter(deps Dependencies) (*gin.Engine, error) {
	optional, required := authChains(deps)

	var healthAlias gin.HandlerFunc
	if deps.Health != nil {
		healthAlias = handlers.Live(deps.Health)
	}

	var mountSwagger func(gin.IRoutes) error
	if deps.SwaggerEnabled {
		mountSwagger = swagger.Mount
	}

	global := globalMiddleware(deps)

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
	optionalServices(&controllerDeps, deps)

	if deps.AdminUsers != nil {
		controllerDeps.AdminUsers = deps.AdminUsers
	}

	if deps.TwoFactor != nil {
		controllerDeps.TwoFactor = deps.TwoFactor
	}

	if deps.AdminLogin != nil {
		controllerDeps.AdminLogin = deps.AdminLogin
	}

	if deps.OAuth != nil {
		controllerDeps.OAuth = deps.OAuth
	}

	if deps.RoleAdmin != nil {
		controllerDeps.RoleAdmin = deps.RoleAdmin
	}

	if deps.Activity != nil {
		controllerDeps.Activity = deps.Activity
	}

	if deps.Notifications != nil {
		controllerDeps.Notifications = deps.Notifications
	}

	if deps.NotificationStream != nil {
		controllerDeps.NotificationStream = deps.NotificationStream
	}

	controllerDeps.SSEPingInterval = deps.SSEPingInterval

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

// authChains builds the bearer middleware. Without an impersonation service, impersonation
// tokens are refused.
func authChains(deps Dependencies) (optional, required gin.HandlersChain) {
	if deps.Auth == nil {
		return nil, nil
	}

	var sessions middleware.ImpersonationVerifier
	if deps.Impersonation != nil {
		sessions = deps.Impersonation
	}

	return gin.HandlersChain{middleware.OptionalBearerAuth(deps.Auth, sessions)},
		gin.HandlersChain{middleware.BearerAuth(deps.Auth, sessions)}
}

// optionalServices copies services that may be absent, keeping a nil service a nil interface.
func optionalServices(controllerDeps *handlers.Deps, deps Dependencies) {
	if deps.Impersonation != nil {
		controllerDeps.Impersonation = deps.Impersonation
	}

	if deps.Newsletter != nil {
		controllerDeps.Newsletter = deps.Newsletter
		controllerDeps.NewsletterProvider = deps.NewsletterProvider
		controllerDeps.NewsletterProvider.SendingEnabled = deps.Newsletter.SendingEnabled()
	}

	if deps.Profiles != nil {
		controllerDeps.Profiles = deps.Profiles
		controllerDeps.Privacy = deps.Profiles
	}

	if deps.Settings != nil {
		controllerDeps.Settings = deps.Settings
		controllerDeps.SettingsValues = deps.Settings
	}

	if deps.PrivacyRequests != nil {
		controllerDeps.PrivacyRequests = deps.PrivacyRequests
	}

	if deps.Consent != nil {
		controllerDeps.Consent = deps.Consent
	}

	if deps.AnalyticsIngest != nil {
		controllerDeps.AnalyticsIngest = deps.AnalyticsIngest
		controllerDeps.AnalyticsIngestPerMinute = deps.AnalyticsIngestPerMinute
		controllerDeps.AnalyticsCountryHeader = deps.AnalyticsCountryHeader
		controllerDeps.TrustedProxies = deps.TrustedProxies
	}

	if deps.AnalyticsReports != nil {
		controllerDeps.AnalyticsReports = deps.AnalyticsReports
	}

	if deps.AnalyticsLive != nil && deps.AnalyticsLiveStreams != nil {
		controllerDeps.AnalyticsLive = deps.AnalyticsLive
		controllerDeps.AnalyticsLiveStreams = deps.AnalyticsLiveStreams
	}
}

// globalMiddleware is the chain every request passes through, outermost first.
func globalMiddleware(deps Dependencies) gin.HandlersChain {
	global := gin.HandlersChain{
		middleware.RequestID(),
		middleware.Tracing(),
		middleware.SecurityHeaders(),
		middleware.AccessLog(deps.Logger),
	}
	if deps.Metrics != nil {
		global = append(global, middleware.Metrics(deps.Metrics))
	}

	global = append(global, middleware.MaxInFlight(deps.MaxInFlight), middleware.Recovery(deps.Logger))
	if deps.Audit != nil {
		global = append(global, middleware.Audit(deps.Audit))
	}

	if deps.CacheBypassHeader {
		global = append(global, middleware.CacheBypass())
	}

	return global
}
