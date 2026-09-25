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

	"github.com/ThreeDotsLabs/watermill"
	"github.com/redis/go-redis/v9"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	"github.com/turahe/blog-api/internal/adapters/outbound/cache"
	"github.com/turahe/blog-api/internal/adapters/outbound/captcha"
	"github.com/turahe/blog-api/internal/adapters/outbound/challenge"
	"github.com/turahe/blog-api/internal/adapters/outbound/imgproxy"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/adapters/outbound/mailqueue"
	"github.com/turahe/blog-api/internal/adapters/outbound/markdown"
	"github.com/turahe/blog-api/internal/adapters/outbound/mediapolicy"
	"github.com/turahe/blog-api/internal/adapters/outbound/notificationbus"
	"github.com/turahe/blog-api/internal/adapters/outbound/notify"
	"github.com/turahe/blog-api/internal/adapters/outbound/oauth"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/postseo"
	"github.com/turahe/blog-api/internal/adapters/outbound/ratelimit"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	auditservice "github.com/turahe/blog-api/internal/core/audit/service"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentports "github.com/turahe/blog-api/internal/core/comment/ports"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	"github.com/turahe/blog-api/internal/core/event"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	notificationports "github.com/turahe/blog-api/internal/core/notification/ports"
	notificationservice "github.com/turahe/blog-api/internal/core/notification/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userports "github.com/turahe/blog-api/internal/core/user/ports"
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

// sseBufferPerConnection is how many notifications one stream queues before dropping.
const sseBufferPerConnection = 512

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
	// NotificationBus carries live notification pushes between replicas; nil without a broker.
	NotificationBus *messaging.Bus
}

type checker struct {
	name     string
	check    func(context.Context) error
	optional bool
}

func (c checker) Name() string                    { return c.name }
func (c checker) Check(ctx context.Context) error { return c.check(ctx) }
func (c checker) Optional() bool                  { return c.optional }

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

	events := NewEvents(cfg, db)
	auth.WithEvents(events)

	inbox := newInbox(cfg, db, clock, logger)
	settings := newSettingsService(db, ids, clock, events, cacheOrNil)
	posts := newPostService(cfg, db, postsRepo, ids, clock, cacheOrNil, inbox, events, settings)
	userSvc := userservice.New(users)

	categories := categoryservice.New(categoriesRepo, ids, clock).WithCache(cacheOrNil)
	if err := categories.RebuildAll(ctx); err != nil {
		logger.Warn("category tree rebuild failed at startup", "error", err)
	}

	tags := tagservice.New(tagsRepo, ids, clock).WithCache(cacheOrNil)
	posts.WithTags(tags).WithSearch(newPostSearch(ctx, cfg, db, logger))

	comments := newCommentService(cfg, db, ids, clock, inbox, events)
	hub, notificationBus := newNotificationStream(ctx, cfg, inbox, logger)

	media, err := newMediaService(ctx, cfg, db, ids, clock, posts, cacheOrNil, events, settings)
	if err != nil {
		return fail(err)
	}

	profiles, avatarMaxBytes := newProfileService(cfg, db, clock, media, cacheOrNil, auth, events)

	enforcer, err := outboundrbac.NewEnforcer(db.GORM)
	if err != nil {
		return fail(fmt.Errorf("create rbac enforcer: %w", err))
	}

	policySync := outboundrbac.NewPolicySync(enforcer, redisClient, cfg.RBACPolicyReloadInterval, logger)
	roleStore := outboundrbac.NewRoleStore(db.GORM, enforcer).WithNotifier(policySync)
	auth.WithRoles(roleStore).WithAccessCheck(enforcer)

	metricsServer, recorder, onAuditDrop := newMetrics(cfg, db, version)
	auditRecorder, activity := newAudit(cfg, db, onAuditDrop, logger)
	newsletter := NewNewsletterService(cfg, db, events, settings, logger)

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Metrics:     recorder,
		MaxInFlight: cfg.HTTPMaxInFlight,
		Audit:       auditRecorder, Activity: activity,
		Logger:         logger,
		Health:         healthservice.New(version, healthCheckers(cfg, db, redisClient)...),
		Auth:           auth,
		AvatarMaxBytes: avatarMaxBytes,
		Users:          userSvc,
		AdminUsers:     auth,
		TwoFactor:      auth, AdminLogin: auth, OAuth: auth,
		RoleAdmin: rbacservice.NewRoleService(roleStore),
		Profiles:  profiles, EmailChange: auth,
		Impersonation: NewImpersonationService(cfg, db, events, impersonationPermissions{enforcer, roleStore}, auth),
		Roles:         users,
		RBAC:          enforcer,
		Posts:         posts, Categories: categories, Tags: tags, Media: media,
		Comments: comments, Notifications: inbox, NotificationStream: hub, SSEPingInterval: cfg.SSEPingInterval,
		Settings: settings, Newsletter: newsletter,
		NewsletterProvider: NewsletterProvider(cfg),
		Consent:            consentservice.New(persistence.NewConsentRepository(db.GORM), ids, clock).WithEvents(events),
		PrivacyRequests:    NewPrivacyService(ctx, cfg, db, auth, events, cacheOrNil, logger).WithModuleErasers(newsletter),
		RateLimiter:        ratelimit.NewRedis(redisClient),
		CommentRates: handlers.CommentRates{
			CreatePerMinute:  cfg.CommentsCreatePerMinute,
			ActionsPerMinute: cfg.CommentsActionsPerMinute,
		},
		LoginPerMinute: cfg.AuthLoginPerMinute, Version: version,
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
		NotificationBus: notificationBus, Server: newServer(cfg, router, hub),
	}, nil
}

// newServer builds the HTTP server. There is no write timeout because notification streams
// stay open; Shutdown tells them to close so it does not wait out the grace period.
func newServer(cfg config.Config, handler nethttp.Handler, hub *realtime.Hub) *nethttp.Server {
	server := &nethttp.Server{
		Addr: cfg.Address, Handler: handler,
		ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
	}
	if hub != nil {
		server.RegisterOnShutdown(hub.Shutdown)
	}

	return server
}

// newNotificationStream wires live notification pushes through the message broker. Without
// MESSAGE_BROKER, or when the broker cannot be reached, it returns nils: the stream answers
// 503 and the inbox keeps working.
func newNotificationStream(
	ctx context.Context,
	cfg config.Config,
	inbox *notificationservice.Inbox,
	logger *slog.Logger,
) (*realtime.Hub, *messaging.Bus) {
	if !cfg.MessagingEnabled() {
		return nil, nil
	}

	bus, err := messaging.OpenBroadcast(ctx, cfg, logger, "api-"+watermill.NewShortUUID())
	if err != nil {
		logger.Warn("notification stream disabled: message broker unavailable", "error", err)
		return nil, nil
	}

	topic := bus.Topic(notificationbus.Topic)
	hub := realtime.NewHub(cfg.SSEMaxConcurrentPerUser, sseBufferPerConnection)

	if err := notificationbus.Consume(ctx, bus.Subscriber, topic, hub.Deliver, logger); err != nil {
		logger.Warn("notification stream disabled: cannot subscribe", "error", err)

		_ = bus.Close()

		return nil, nil
	}

	inbox.OnCreated(notificationbus.NewPublisher(bus.Publisher, topic, logger).Publish)

	return hub, bus
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
	events event.Unit,
	settings *settingsservice.Service,
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

	media := mediaservice.New(
		mediaRepo,
		objectStorage,
		ids,
		clock,
		cfg.S3Disk,
		cfg.MediaAllowedMIMETypes,
		cfg.MediaMaxUploadBytes,
		cfg.MediaPresignTTL,
	).WithCache(readCache).WithEvents(events).WithPolicy(mediapolicy.New(settings))

	if cfg.MediaTransformsEnabled() {
		signer, err := imgproxy.New(imgproxy.Config{
			BaseURL: cfg.ImgproxyURL,
			Key:     cfg.ImgproxyKey,
			Salt:    cfg.ImgproxySalt,
			Bucket:  cfg.S3Bucket,
			TTL:     cfg.MediaTransformURLTTL,
		})
		if err != nil {
			return nil, fmt.Errorf("media transforms: %w", err)
		}

		media.WithTransforms(signer, cfg.MediaTransformWidths)
	}

	return media, nil
}

// newPostService wires posts with revisions and SEO; media and tags are attached later.
func newPostService(
	cfg config.Config,
	db *database.Database,
	repo *persistence.PostRepository,
	ids system.UUIDGenerator,
	clock system.Clock,
	readCache readcache.Cache,
	inbox *notificationservice.Inbox,
	events event.Unit,
	settings *settingsservice.Service,
) *postservice.PostService {
	return postservice.New(repo, ids, clock).WithCache(readCache).WithNotifier(inbox).WithEvents(events).
		WithRevisions(persistence.NewPostRevisionRepository(db.GORM)).
		WithSEO(persistence.NewPostSEORepository(db.GORM), postseo.NewDefaults(settings), newSEOImageURLs(cfg, db))
}

// newSEOImageURLs resolves social card images; without media storage posts have no images.
func newSEOImageURLs(cfg config.Config, db *database.Database) *postseo.ImageURLs {
	if !cfg.MediaEnabled() {
		return nil
	}

	var widths []int
	if cfg.MediaTransformsEnabled() {
		widths = cfg.MediaTransformWidths
	}

	return postseo.NewImageURLs(persistence.NewMediaRepository(db.GORM), cfg.AppPublicURL, widths, cfg.S3PublicBaseURL)
}

// newProfileService wires profiles and privacy; avatars are enabled (non-zero max bytes) only with media storage.
func newProfileService(
	cfg config.Config,
	db *database.Database,
	clock system.Clock,
	media mediaports.Service,
	readCache readcache.Cache,
	verifier userports.PasswordVerifier,
	events event.Unit,
) (*userservice.ProfileService, int64) {
	profiles := userservice.NewProfileService(persistence.NewProfileRepository(db.GORM), clock).
		WithCache(readCache).WithPrivacy(verifier, events)
	if media == nil {
		return profiles, 0
	}

	return profiles.WithAvatars(media, cfg.AvatarMaxBytes), cfg.AvatarMaxBytes
}

// newSettingsService serves the admin settings catalogue; updates record settings.updated in the outbox.
func newSettingsService(
	db *database.Database, ids system.UUIDGenerator, clock system.Clock, events event.Unit, readCache readcache.Cache,
) *settingsservice.Service {
	repo := persistence.NewSettingsRepository(db.GORM)

	return settingsservice.New(repo, settingsdomain.DefaultCatalogue(), ids, clock).WithEvents(events).WithCache(readCache)
}

func newCommentService(
	cfg config.Config,
	db *database.Database,
	ids system.UUIDGenerator,
	clock system.Clock,
	notifier commentports.Notifier,
	events event.Unit,
) *commentservice.Service {
	commentCfg := commentservice.Config{
		GuestEnabled:    cfg.CommentsGuestEnabled,
		RequireApproval: cfg.CommentsRequireApproval,
		EditWindow:      cfg.CommentsEditWindow,
		FlagThreshold:   cfg.CommentsFlagThreshold,
		Renderer:        markdown.New(),
		Notifier:        notifier,
		Events:          events,
		IdentityHasher:  identityHasher(cfg),
	}
	if cfg.TurnstileSecretKey != "" {
		commentCfg.Captcha = captcha.NewTurnstile(cfg.TurnstileSecretKey, "", nil)
	}

	return commentservice.New(persistence.NewCommentRepository(db.GORM), ids, clock, commentCfg)
}

// NewEvents runs service writes in transactions and, when a message broker is configured,
// stores their domain events in the outbox for app worker to relay. Without a broker the
// events are dropped, so the outbox does not grow with nothing to drain it.
func NewEvents(cfg config.Config, db *database.Database) event.Unit {
	events := event.Unit{Tx: persistence.NewTransactor(db.GORM), Recorder: event.Discard{}}
	if cfg.MessagingEnabled() {
		events.Recorder = persistence.NewOutboxRepository(db.GORM)
	}

	return events
}

// newInbox stores in-app notifications for comment replies, moderation, and publications.
func newInbox(cfg config.Config, db *database.Database, clock system.Clock, logger *slog.Logger) *notificationservice.Inbox {
	repo := persistence.NewNotificationRepository(db.GORM)

	return notificationservice.NewInbox(repo, repo, clock, logger, cfg.AppPublicURL).
		WithTemplates(persistence.NewNotificationTemplateRepository(db.GORM))
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

	box, err := newSecretBox(cfg, logger)
	if err != nil {
		return nil, err
	}

	auth := authservice.New(users, sessions, resets, password.New(), tokens, clock, ids, authservice.Config{
		AccessTTL:  cfg.AccessTokenTTL,
		RefreshTTL: cfg.RefreshTokenTTL,
	}, nil).WithEmailChange(newNotifier(cfg, db, box, logger), cacheOrNil)
	if cfg.AuthLoginMaxFailures > 0 {
		auth.WithLoginAttempts(ratelimit.NewLoginLockout(redisClient, cfg.AuthLoginMaxFailures, cfg.AuthLoginLockout))
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
	box, err := NewSecretBox(cfg)
	if err != nil || box == nil {
		if err == nil {
			logger.Warn("APP_ENCRYPTION_KEY is not set; two-factor enrollment is disabled and stored IP hashes are unkeyed")
		}

		return nil, err
	}

	return box, nil
}

// identityHasher returns the APP_ENCRYPTION_KEY box that keys stored IP hashes, or a nil
// interface (plain SHA-256) when the key is unset. Config validation rejects a malformed key.
func identityHasher(cfg config.Config) commentports.IdentityHasher {
	box, err := NewSecretBox(cfg)
	if err != nil || box == nil {
		return nil
	}

	return box
}

// NewSecretBox returns the APP_ENCRYPTION_KEY box, or nil when the key is unset.
func NewSecretBox(cfg config.Config) (*secretbox.Box, error) {
	if cfg.EncryptionKey == "" {
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
		readcache.Settings:   cfg.CacheTTLSettings,
	}
}

// newNotifier sends account emails over SMTP, or logs them when SMTP is not configured. With
// a message broker and APP_ENCRYPTION_KEY, emails are queued as encrypted outbox commands for
// app worker, falling back to SMTP when a command cannot be stored.
func newNotifier(cfg config.Config, db *database.Database, box authports.SecretBox, logger *slog.Logger) authports.EmailChangeNotifier {
	mailer := NewTransactionalMailer(cfg, db, box, logger)
	if mailer == nil {
		return notify.NewLog(logger)
	}

	switch {
	case cfg.MessagingEnabled() && box != nil:
		logger.Info("email notifications queued for app worker", "host", cfg.SMTPHost, "port", cfg.SMTPPort)
	case cfg.MessagingEnabled():
		logger.Warn("APP_ENCRYPTION_KEY is not set; emails are sent inline instead of by app worker")
	default:
		logger.Info("email notifications via smtp", "host", cfg.SMTPHost, "port", cfg.SMTPPort)
	}

	return notificationservice.New(mailer, logger, cfg.AppPublicURL).
		WithTemplates(persistence.NewNotificationTemplateRepository(db.GORM))
}

// NewTransactionalMailer returns the SMTP mailer for account and confirmation emails, wrapped
// to queue encrypted commands through the outbox when a broker and box are available. It
// returns nil when SMTP_HOST is unset or invalid.
func NewTransactionalMailer(cfg config.Config, db *database.Database, box mailqueue.Box, logger *slog.Logger) notificationports.Mailer {
	smtp, err := NewMailer(cfg)
	if err != nil {
		logger.Error("smtp mailer disabled", "error", err)
	}

	if smtp == nil {
		return nil
	}

	if cfg.MessagingEnabled() && box != nil {
		return mailqueue.New(persistence.NewOutboxRepository(db.GORM), box, smtp, logger)
	}

	return smtp
}

// NewMailer returns the SMTP mailer, or nil when SMTP_HOST is unset.
func NewMailer(cfg config.Config) (*mail.SMTP, error) {
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return nil, nil
	}

	mailer, err := mail.NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom)
	if err != nil {
		return nil, fmt.Errorf("create smtp mailer: %w", err)
	}

	return mailer, nil
}

// WorkerHealthCheckers probes what the worker cannot run without: the database and the broker.
func WorkerHealthCheckers(cfg config.Config, db *database.Database) []healthports.Checker {
	return []healthports.Checker{
		checker{name: "database", check: func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			return db.SQL.PingContext(checkCtx)
		}},
		checker{name: "messaging", check: func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			return messaging.Probe(checkCtx, cfg)
		}},
	}
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
		// The API keeps accepting writes while the broker is down (events wait in the outbox),
		// so a broker outage is reported without taking replicas out of rotation.
		checkers = append(checkers, checker{name: "messaging", optional: true, check: func(ctx context.Context) error {
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

	if err := r.NotificationBus.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close notification bus: %w", err))
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
