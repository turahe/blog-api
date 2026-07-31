package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/redis/go-redis/v9"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	redisplatform "github.com/turahe/blog-api/internal/platform/redis"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"github.com/turahe/blog-api/internal/platform/system"
)

type Runtime struct {
	Config     config.Config
	Database   *database.Database
	Redis      *redis.Client
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

	tokenService, err := jwttoken.New(cfg.SessionKey, cfg.JWTIssuer)
	if err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		return nil, fmt.Errorf("create token service: %w", err)
	}

	users := persistence.NewUserRepository(db.GORM)
	sessions := persistence.NewSessionRepository(db.GORM)
	resets := persistence.NewResetTokenRepository(db.GORM)
	postsRepo := persistence.NewPostRepository(db.GORM)
	categoriesRepo := persistence.NewCategoryRepository(db.GORM)
	tagsRepo := persistence.NewTagRepository(db.GORM)
	hasher := password.New(0)
	clock := system.Clock{}
	ids := system.UUIDGenerator{}

	auth := authservice.New(users, sessions, resets, hasher, tokenService, clock, ids, authservice.Config{
		AccessTTL:  cfg.AccessTokenTTL,
		RefreshTTL: cfg.RefreshTokenTTL,
	}, nil)
	posts := postservice.New(postsRepo, ids, clock)
	userSvc := userservice.New(users)
	categories := categoryservice.New(categoriesRepo, ids, clock)
	tags := tagservice.New(tagsRepo, ids, clock)
	posts.WithTags(tags)

	var media mediaports.Service
	var mediaRepo *persistence.MediaRepository
	if cfg.MediaEnabled() {
		objectStorage, err := storage.NewS3(ctx, cfg)
		if err != nil {
			_ = redisClient.Close()
			_ = db.Close()
			return nil, fmt.Errorf("create media storage: %w", err)
		}
		mediaRepo = persistence.NewMediaRepository(db.GORM)
		media = mediaservice.New(
			mediaRepo,
			objectStorage,
			ids,
			clock,
			cfg.S3Disk,
			cfg.MediaAllowedMIMETypes,
			cfg.MediaMaxUploadBytes,
			cfg.MediaPresignTTL,
		)
		posts.WithMedia(persistence.NewPostMediaRepository(db.GORM), mediaRepo)
	}

	enforcer, err := outboundrbac.NewEnforcer(db.GORM)
	if err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		return nil, fmt.Errorf("create rbac enforcer: %w", err)
	}

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
	health := healthservice.New(version, checkers...)
	router, err := httpadapter.NewRouter(httpadapter.Dependencies{
		Logger:         logger,
		Health:         health,
		Auth:           auth,
		Users:          userSvc,
		Roles:          users,
		RBAC:           enforcer,
		Posts:          posts,
		Categories:     categories,
		Tags:           tags,
		Media:          media,
		Version:        version,
		TrustedProxies: cfg.TrustedProxies,
	})
	if err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		return nil, fmt.Errorf("create router: %w", err)
	}

	return &Runtime{
		Config: cfg, Database: db, Redis: redisClient,
		Auth: auth, Users: userSvc, Posts: posts, Categories: categories, Media: media,
		Server: &nethttp.Server{
			Addr: cfg.Address, Handler: router,
			ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
		},
	}, nil
}

func (r *Runtime) Close() error {
	var first error
	if r.Redis != nil {
		if err := r.Redis.Close(); err != nil {
			first = err
		}
	}
	if err := r.Database.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
