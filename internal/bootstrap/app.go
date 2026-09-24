// Package bootstrap wires configuration, infrastructure, and services into a runnable application.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/redis/go-redis/v9"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/handlers"
	"github.com/turahe/blog-api/internal/adapters/outbound/cache"
	"github.com/turahe/blog-api/internal/adapters/outbound/notify"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/ratelimit"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	redisplatform "github.com/turahe/blog-api/internal/platform/redis"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"github.com/turahe/blog-api/internal/platform/system"
)

// Runtime holds the opened infrastructure, core services, and HTTP server.
type Runtime struct {
	Config     config.Config
	Database   *database.Database
	Redis      *redis.Client
	Cache      *cache.Redis // nil when CACHE_ENABLED=false
	Server     *nethttp.Server
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

	closeAll := func() {
		_ = redisClient.Close()
		_ = db.Close()
	}

	tokenService, err := jwttoken.New(cfg.JWTPrivateKey, cfg.JWTPublicKey, cfg.SessionKey, cfg.JWTIssuer)
	if err != nil {
		closeAll()

		return nil, fmt.Errorf("create token service: %w", err)
	}

	users := persistence.NewUserRepository(db.GORM)
	sessions := persistence.NewSessionRepository(db.GORM)
	resets := persistence.NewResetTokenRepository(db.GORM)
	postsRepo := persistence.NewPostRepository(db.GORM)
	categoriesRepo := persistence.NewCategoryRepository(db.GORM)
	tagsRepo := persistence.NewTagRepository(db.GORM)
	hasher := password.New()
	clock := system.Clock{}
	ids := system.UUIDGenerator{}

	readCache := newReadCache(cfg, redisClient, logger)

	var cacheOrNil readcache.Cache // stays a nil interface, not a typed nil, when disabled
	if readCache != nil {
		cacheOrNil = readCache
	}

	auth := authservice.New(users, sessions, resets, hasher, tokenService, clock, ids, authservice.Config{
		AccessTTL:  cfg.AccessTokenTTL,
		RefreshTTL: cfg.RefreshTokenTTL,
	}, nil).WithEmailChange(notify.NewLog(logger), cacheOrNil)

	posts := postservice.New(postsRepo, ids, clock).WithCache(cacheOrNil)
	userSvc := userservice.New(users)

	categories := categoryservice.New(categoriesRepo, ids, clock).WithCache(cacheOrNil)
	if err := categories.RebuildAll(ctx); err != nil {
		logger.Warn("category tree rebuild failed at startup", "error", err)
	}

	tags := tagservice.New(tagsRepo, ids, clock).WithCache(cacheOrNil)
	posts.WithTags(tags)

	comments := commentservice.New(persistence.NewCommentRepository(db.GORM), ids, clock, commentservice.Config{
		GuestEnabled:    cfg.CommentsGuestEnabled,
		RequireApproval: cfg.CommentsRequireApproval,
		EditWindow:      cfg.CommentsEditWindow,
		FlagThreshold:   cfg.CommentsFlagThreshold,
	})

	media, err := newMediaService(ctx, cfg, db, ids, clock, posts, cacheOrNil)
	if err != nil {
		closeAll()

		return nil, err
	}

	profiles, avatarMaxBytes := newProfileService(cfg, db, clock, media, cacheOrNil)

	enforcer, err := outboundrbac.NewEnforcer(db.GORM)
	if err != nil {
		closeAll()

		return nil, fmt.Errorf("create rbac enforcer: %w", err)
	}

	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:         logger,
		Health:         healthservice.New(version, healthCheckers(db, redisClient)...),
		Auth:           auth,
		AvatarMaxBytes: avatarMaxBytes,
		Users:          userSvc,
		Profiles:       profiles,
		EmailChange:    auth,
		Roles:          users,
		RBAC:           enforcer,
		Posts:          posts,
		Categories:     categories,
		Tags:           tags,
		Media:          media,
		Comments:       comments,
		RateLimiter:    ratelimit.NewRedis(redisClient),
		CommentRates: handlers.CommentRates{
			CreatePerMinute:  cfg.CommentsCreatePerMinute,
			ActionsPerMinute: cfg.CommentsActionsPerMinute,
		},
		Version:           version,
		TrustedProxies:    cfg.TrustedProxies,
		SwaggerEnabled:    cfg.SwaggerEnabled,
		CacheBypassHeader: cfg.CacheEnabled && cfg.CacheBypassHeader,
	})
	if err != nil {
		closeAll()

		return nil, fmt.Errorf("create router: %w", err)
	}

	return &Runtime{
		Config: cfg, Database: db, Redis: redisClient, Cache: readCache,
		Auth: auth, Users: userSvc, Posts: posts, Categories: categories, Media: media,
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

func healthCheckers(db *database.Database, redisClient *redis.Client) []healthports.Checker {
	return []healthports.Checker{
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
}

// Close releases Redis and database connections.
func (r *Runtime) Close() error {
	var errs []error

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
