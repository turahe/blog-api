// Package bootstrap wires configuration, infrastructure, and services into a runnable application.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/outbound/cache"
	"github.com/turahe/blog-api/internal/adapters/outbound/challenge"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/adapters/outbound/notify"
	"github.com/turahe/blog-api/internal/adapters/outbound/oauth"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/ratelimit"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
	"github.com/turahe/blog-api/internal/platform/metrics"
	redisplatform "github.com/turahe/blog-api/internal/platform/redis"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"github.com/turahe/blog-api/internal/platform/security/secretbox"
	"github.com/turahe/blog-api/internal/platform/system"
)

// auditFlushTimeout bounds how long Close waits for queued audit entries.
const auditFlushTimeout = 5 * time.Second

// Runtime holds the opened infrastructure, core services, and HTTP server.
type Runtime struct {
	Config   config.Config
	Database *database.Database
	Redis    *redis.Client
	Cache    *cache.Redis // nil when CACHE_ENABLED=false
	Server   *nethttp.Server
	// MetricsServer serves Prometheus /metrics; nil when METRICS_ADDR is empty.
	MetricsServer *nethttp.Server
	// PolicySync reloads the Casbin policy on peer announcements and on an interval;
	// the caller starts it and Close stops it.
	PolicySync *outboundrbac.PolicySync
	// Audit writes audit entries in the background; Close flushes it.
	Audit      *auditservice.Recorder
	Auth       *authservice.AuthService
	Users      *userservice.UserService
	Posts      *postservice.PostService
	Categories *categoryservice.CategoryService
	Media      mediaports.Service
}

type checker struct {
	name  string
	check func(context.Context) error
}

func (c checker) Name() string                    { return c.name }
func (c checker) Check(ctx context.Context) error { return c.check(ctx) }

// NewRuntime opens the database and Redis, wires services, and builds the HTTP server.
func NewRuntime(ctx context.Context, cfg config.Config, logger *slog.Logger, version string) (*Runtime, error) {
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}

	redisClient, err := redisplatform.Open(ctx, cfg.RedisURL())
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	fail := func(err error) (*Runtime, error) {
		_ = redisClient.Close()
		_ = db.Close()

		return nil, err
	}

	users := persistence.NewUserRepository(db.GORM)
	sessions := persistence.NewSessionRepository(db.GORM)
	resets := persistence.NewResetTokenRepository(db.GORM)
	postsRepo := persistence.NewPostRepository(db.GORM)
	categoriesRepo := persistence.NewCategoryRepository(db.GORM)
	tagsRepo := persistence.NewTagRepository(db.GORM)
	clock := system.Clock{}
	ids := system.UUIDGenerator{}

	readCache := newReadCache(cfg, redisClient, logger)

	var cacheOrNil readcache.Cache // stays a nil interface, not a typed nil, when disabled
	if readCache != nil {
		cacheOrNil = readCache
	}

	auth, err := newAuthService(cfg, db, redisClient, users, sessions, resets, clock, ids, cacheOrNil, logger)
	if err != nil {
		return fail(err)
	}

	posts := postservice.New(postsRepo, ids, clock).WithCache(cacheOrNil)
	userSvc := userservice.New(users)

	categories := categoryservice.New(categoriesRepo, ids, clock).WithCache(cacheOrNil)
	if err := categories.RebuildAll(ctx); err != nil {
		logger.Warn("category tree rebuild failed at startup", "error", err)
	}

	tags := tagservice.New(tagsRepo, ids, clock).WithCache(cacheOrNil)
	posts.WithTags(tags)

	comments := newCommentService(cfg, db, ids, clock)

	media, err := newMediaService(ctx, cfg, db, ids, clock, posts, cacheOrNil)
	if err != nil {
		return fail(err)
	}

	profiles, avatarMaxBytes := newProfileService(cfg, db, clock, media, cacheOrNil)

	enforcer, err := outboundrbac.NewEnforcer(db.GORM)
	if err != nil {
		return fail(fmt.Errorf("create rbac enforcer: %w", err))
	}

	policySync := outboundrbac.NewPolicySync(enforcer, redisClient, cfg.RBACPolicyReloadInterval, logger)
	roleStore := outboundrbac.NewRoleStore(db.GORM, enforcer).WithNotifier(policySync)
	auth.WithRoles(roleStore).WithAccessCheck(enforcer)

	metricsServer, recorder, onAuditDrop := newMetrics(cfg, db, version)
	auditRecorder, activity := newAudit(cfg, db, onAuditDrop, logger)

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Metrics: recorder,
		Audit:   auditRecorder, Activity: activity,
		Logger:         logger,
		Health:         healthservice.New(version, healthCheckers(cfg, db, redisClient)...),
		Auth:           auth,
		AvatarMaxBytes: avatarMaxBytes,
		Users:          userSvc,
		AdminUsers:     auth,
		TwoFactor:      auth, AdminLogin: auth, OAuth: auth,
		RoleAdmin:   rbacservice.NewRoleService(roleStore),
		Profiles:    profiles,
		EmailChange: auth,
		Roles:       users,
		RBAC:        enforcer,
		Posts:       posts,
		Categories:  categories,
		Tags:        tags,
		Media:       media,
		Comments:    comments,
		RateLimiter: ratelimit.NewRedis(redisClient),
		CommentRates: handlers.CommentRates{
			CreatePerMinute:  cfg.CommentsCreatePerMinute,
			ActionsPerMinute: cfg.CommentsActionsPerMinute,
		},
		LoginPerMinute:    cfg.AuthLoginPerMinute,
		Version:           version,
		TrustedProxies:    cfg.TrustedProxies,
		SwaggerEnabled:    cfg.SwaggerEnabled,
		CacheBypassHeader: cfg.CacheEnabled && cfg.CacheBypassHeader,
	})
	if err != nil {
		return fail(fmt.Errorf("create router: %w", err))
	}

	return &Runtime{
		Config: cfg, Database: db, Redis: redisClient, Cache: readCache,
		Auth: auth, Users: userSvc, Posts: posts, Categories: categories, Media: media,
		MetricsServer: metricsServer, PolicySync: policySync, Audit: auditRecorder,
		Server: &nethttp.Server{
			Addr: cfg.Address, Handler: router,
			ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
		},
	}, nil
}

// newMediaService returns nil when media storage is not configured; otherwise it
// also attaches post media persistence to posts.
func newMediaService(
	ctx context.Context,
	cfg config.Config,
	db *database.Database,
	ids system.UUIDGenerator,
	clock system.Clock,
	posts *postservice.PostService,
	readCache readcache.Cache,
) (mediaports.Service, error) {
	if !cfg.MediaEnabled() {
		return nil, nil
	}

	objectStorage, err := storage.NewS3(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create media storage: %w", err)
	}

	mediaRepo := persistence.NewMediaRepository(db.GORM)
	posts.WithMedia(persistence.NewPostMediaRepository(db.GORM), mediaRepo)

	return mediaservice.New(
		mediaRepo,
		objectStorage,
		ids,
		clock,
		cfg.S3Disk,
		cfg.MediaAllowedMIMETypes,
		cfg.MediaMaxUploadBytes,
		cfg.MediaPresignTTL,
	).WithCache(readCache), nil
}

// newProfileService wires profiles; avatars are enabled (non-zero max bytes) only with media storage.
func newProfileService(
	cfg config.Config,
	db *database.Database,
	clock system.Clock,
	media mediaports.Service,
	readCache readcache.Cache,
) (*userservice.ProfileService, int64) {
	profiles := userservice.NewProfileService(persistence.NewProfileRepository(db.GORM), clock).WithCache(readCache)
	if media == nil {
		return profiles, 0
	}

	return profiles.WithAvatars(media, cfg.AvatarMaxBytes), cfg.AvatarMaxBytes
}

func newCommentService(cfg config.Config, db *database.Database, ids system.UUIDGenerator, clock system.Clock) *commentservice.Service {
	return commentservice.New(persistence.NewCommentRepository(db.GORM), ids, clock, commentservice.Config{
		GuestEnabled:    cfg.CommentsGuestEnabled,
		RequireApproval: cfg.CommentsRequireApproval,
		EditWindow:      cfg.CommentsEditWindow,
		FlagThreshold:   cfg.CommentsFlagThreshold,
	})
}

// newMetrics returns the Prometheus server, request recorder, and audit drop
// counter, or nils when METRICS_ADDR is empty.
func newMetrics(cfg config.Config, db *database.Database, version string) (*nethttp.Server, middleware.MetricsRecorder, func(int)) {
	if cfg.MetricsAddr == "" {
		return nil, nil, nil
	}

	appMetrics := metrics.New(db.SQL, version)

	return appMetrics.NewServer(cfg.MetricsAddr), appMetrics, appMetrics.AuditDropped
}

// newAudit starts the background audit writer and the activity query service.
func newAudit(cfg config.Config, db *database.Database, onDrop func(int), logger *slog.Logger) (*auditservice.Recorder, *auditservice.Activity) {
	repo := persistence.NewAuditRepository(db.GORM)
	recorder := auditservice.NewRecorder(repo, logger, auditservice.RecorderOptions{
		QueueSize: cfg.AuditQueueSize,
		OnDrop:    onDrop,
	})

	return recorder, auditservice.NewActivity(repo)
}

// newAuthService builds the auth service with lockout, email change, and two-factor wired.
func newAuthService(
	cfg config.Config, db *database.Database, redisClient *redis.Client,
	users *persistence.UserRepository, sessions *persistence.SessionRepository, resets *persistence.ResetTokenRepository,
	clock system.Clock, ids system.UUIDGenerator, cacheOrNil readcache.Cache, logger *slog.Logger,
) (*authservice.AuthService, error) {
	tokens, err := jwttoken.New(cfg.JWTPrivateKey, cfg.JWTPublicKey, cfg.SessionKey, cfg.JWTIssuer)
	if err != nil {
		return nil, fmt.Errorf("create token service: %w", err)
	}

	auth := authservice.New(users, sessions, resets, password.New(), tokens, clock, ids, authservice.Config{
		AccessTTL:  cfg.AccessTokenTTL,
		RefreshTTL: cfg.RefreshTokenTTL,
	}, nil).WithEmailChange(newNotifier(cfg, logger, persistence.NewNotificationTemplateRepository(db.GORM)), cacheOrNil)
	if cfg.AuthLoginMaxFailures > 0 {
		auth.WithLoginAttempts(ratelimit.NewLoginLockout(redisClient, cfg.AuthLoginMaxFailures, cfg.AuthLoginLockout))
	}

	box, err := newSecretBox(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Always wired, even without a key, so enrolled accounts fail closed.
	auth.WithTwoFactor(persistence.NewTwoFactorRepository(db.GORM), box, challenge.New(redisClient), cfg.TwoFactorIssuer)
	auth.WithOAuth(oauthProviders(cfg), persistence.NewOAuthIdentityRepository(db.GORM),
		challenge.NewOAuthStates(redisClient), cfg.OAuthRedirectURIs)

	return auth, nil
}

// oauthProviders returns the providers whose client credentials are configured.
func oauthProviders(cfg config.Config) map[string]authports.OAuthProvider {
	providers := map[string]authports.OAuthProvider{}
	if cfg.OAuthGoogleClientID != "" {
		providers[oauth.Google] = oauth.NewGoogle(
			oauth.Credentials{ClientID: cfg.OAuthGoogleClientID, ClientSecret: cfg.OAuthGoogleClientSecret}, oauth.GoogleEndpoints)
	}

	if cfg.OAuthGitHubClientID != "" {
		providers[oauth.GitHub] = oauth.NewGitHub(
			oauth.Credentials{ClientID: cfg.OAuthGitHubClientID, ClientSecret: cfg.OAuthGitHubClientSecret}, oauth.GitHubEndpoints)
	}

	return providers
}

// newSecretBox returns the APP_ENCRYPTION_KEY box, or a nil interface when the key is unset.
func newSecretBox(cfg config.Config, logger *slog.Logger) (authports.SecretBox, error) {
	if cfg.EncryptionKey == "" {
		logger.Warn("APP_ENCRYPTION_KEY is not set; two-factor enrollment is disabled")
		return nil, nil
	}

	key, err := secretbox.ParseKey(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY: %w", err)
	}

	box, err := secretbox.New(key)
	if err != nil {
		return nil, fmt.Errorf("create secret box: %w", err)
	}

	return box, nil
}

// newReadCache returns the Redis public-read cache, or nil when CACHE_ENABLED=false.
func newReadCache(cfg config.Config, client *redis.Client, logger *slog.Logger) *cache.Redis {
	if !cfg.CacheEnabled {
		return nil
	}

	return cache.NewRedis(client, CacheTTLs(cfg), logger)
}

// CacheTTLs maps each read family to its configured TTL.
func CacheTTLs(cfg config.Config) map[readcache.Family]time.Duration {
	return map[readcache.Family]time.Duration{
		readcache.Posts:      cfg.CacheTTLPosts,
		readcache.Categories: cfg.CacheTTLCategories,
		readcache.Tags:       cfg.CacheTTLTags,
		readcache.Users:      cfg.CacheTTLUsers,
	}
}

func newNotifier(cfg config.Config, logger *slog.Logger, templates *persistence.NotificationTemplateRepository) authports.EmailChangeNotifier {
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return notify.NewLog(logger)
	}

	mailer, err := mail.NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom)
	if err != nil {
		logger.Error("smtp notifier disabled", "error", err)

		return notify.NewLog(logger)
	}

	logger.Info("email notifications via smtp", "host", cfg.SMTPHost, "port", cfg.SMTPPort)

	return notificationservice.New(mailer, logger, cfg.AppPublicURL).WithTemplates(templates)
}

func healthCheckers(cfg config.Config, db *database.Database, redisClient *redis.Client) []healthports.Checker {
	checkers := []healthports.Checker{
		checker{name: "database", check: func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			return db.SQL.PingContext(checkCtx)
		}},
		checker{name: "redis", check: func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			return redisClient.Ping(checkCtx).Err()
		}},
	}

	if cfg.MessagingEnabled() {
		checkers = append(checkers, checker{name: "messaging", check: func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			return messaging.Probe(checkCtx, cfg)
		}})
	}

	return checkers
}

// Close flushes queued audit entries and releases Redis and database connections.
func (r *Runtime) Close() error {
	var errs []error

	if r.PolicySync != nil {
		r.PolicySync.Stop()
	}

	if r.Audit != nil {
		ctx, cancel := context.WithTimeout(context.Background(), auditFlushTimeout)
		if err := r.Audit.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush audit log: %w", err))
		}

		cancel()
	}

	if r.Redis != nil {
		if err := r.Redis.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close redis: %w", err))
		}
	}

	if err := r.Database.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close database: %w", err))
	}

	return errors.Join(errs...)
}
