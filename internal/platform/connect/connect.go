// Package connect installs github.com/turahe/pkg as the process-wide database,
// logger, Sentry, JWT, Redis, and tracing stack for this API.
package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	pkgconfig "github.com/turahe/pkg/config"
	pkgdb "github.com/turahe/pkg/database"
	pkgjwt "github.com/turahe/pkg/jwt"
	pklogger "github.com/turahe/pkg/logger"
	"github.com/turahe/pkg/otelx"
	pkgredis "github.com/turahe/pkg/redis"
	gormlogger "gorm.io/gorm/logger"

	"github.com/turahe/blog-api/internal/platform/config"
)

const redacted = "[REDACTED]"

// Install publishes cfg on the turahe/pkg config singleton and starts its logger.
// Call once at process start, before Sentry, Redis, or tracing.
func Install(cfg config.Config, version string) {
	pkgconfig.Config = Configuration(cfg, version)
	pklogger.Init(loggerConfig(cfg, version))
}

// Configuration returns the turahe/pkg configuration carried by cfg.
func Configuration(cfg config.Config, version string) *pkgconfig.Configuration {
	shared := cfg.PkgConfig(version)

	return &shared
}

// SentryConfig is the turahe/pkg Sentry block for cfg.
func SentryConfig(cfg config.Config, version string) pkgconfig.SentryConfiguration {
	return cfg.PkgConfig(version).Sentry
}

// OpenDatabase opens Postgres, or Cloud SQL Postgres, through github.com/turahe/pkg/database.
// That package only represents sslmode disable and require. Other modes are rejected
// here so a direct connection is not opened with certificate checks silently dropped.
func OpenDatabase(ctx context.Context, cfg config.Config) (*pkgdb.Database, error) {
	if _, err := config.NormalizeDBDriver(cfg.DBDriver); err != nil {
		return nil, err
	}

	if cfg.UsesCloudSQL() {
		if cfg.DBName == "" {
			return nil, errors.New("DB_NAME is required for Cloud SQL")
		}

		if cfg.DBUser == "" {
			return nil, errors.New("DB_USER is required for Cloud SQL")
		}

		if err := validateCredentialsSource(cfg.DBGoogleCredentialsSource); err != nil {
			return nil, err
		}
	} else if err := validateSSLMode(cfg.DBSSLMode); err != nil {
		return nil, err
	}

	restore, err := useGoogleCredentials(cfg)
	if err != nil {
		return nil, err
	}
	defer restore()

	dbCfg := cfg.PkgConfig("").Database
	idle := cfg.DBMaxIdleTime
	if idle <= 0 {
		idle = 10 * time.Minute
	}

	life := cfg.DBMaxLifetime
	if life <= 0 {
		life = 30 * time.Minute
	}

	return pkgdb.NewContext(ctx, &dbCfg, pkgdb.Options{
		UseIAM:       cfg.DBIAMAuthEnabled,
		UsePrivateIP: cfg.DBPrivateIPEnabled,
		LogLevel:     gormlogger.Silent,
		MaxOpenConns: cfg.DBMaxOpen,
		MaxIdleConns: cfg.DBMaxIdle,
		ConnMaxLife:  life,
		ConnMaxIdle:  idle,
		PingTimeout:  5 * time.Second,
	}, pkgdb.WithOpenTelemetry(false))
}

// LoadJWTKeys checks that turahe/pkg can load the ES256 pair used to issue access tokens.
// Token claims stay in internal/platform/security/jwt; that package carries email,
// session, and impersonation claims the shared manager does not sign.
func LoadJWTKeys(privatePEM, publicPEM, issuer string) error {
	_, err := pkgjwt.NewManager(context.Background(), &pkgconfig.Configuration{
		Server: pkgconfig.ServerConfiguration{
			JWTSigningAlgorithm: "ES256",
			JWTPrivateKeyPEM:    []byte(privatePEM),
			JWTPublicKeyPEM:     []byte(publicPEM),
			JWTIssuer:           issuer,
		},
	})

	return err
}

// UseRedis points the turahe/pkg Redis client at rawURL.
func UseRedis(rawURL string) error {
	options, err := goredis.ParseURL(rawURL)
	if err != nil {
		return err
	}

	host, port, err := net.SplitHostPort(options.Addr)
	if err != nil {
		return err
	}

	current := pkgconfig.Config
	if current == nil {
		current = &pkgconfig.Configuration{}
		pkgconfig.Config = current
	}

	current.Redis = pkgconfig.RedisConfiguration{
		Enabled:  true,
		Host:     host,
		Port:     port,
		Password: options.Password,
		DB:       options.DB,
	}

	return nil
}

// OpenRedis connects the shared Redis client. The returned client is that singleton.
func OpenRedis(ctx context.Context, rawURL string) error {
	if err := UseRedis(rawURL); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := pkgredis.Setup(); err != nil {
		return err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := pkgredis.GetRedis().Ping(pingCtx).Err(); err != nil {
		_ = pkgredis.Close()

		return err
	}

	return nil
}

// InitTracing starts the global tracer when OTEL_TRACES_EXPORTER is gcp or an OTLP
// endpoint is set. The returned function is safe to call when tracing is off.
func InitTracing(ctx context.Context, cfg config.Config, version string) func(context.Context) error {
	otelCfg := cfg.PkgConfig(version).OpenTelemetry
	noop := func(context.Context) error { return nil }

	if !otelx.TracingEnabled(otelCfg) {
		return noop
	}

	shutdown, enabled := otelx.Init(ctx, otelCfg)
	if !enabled {
		return noop
	}

	return shutdown
}

func validateSSLMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "disable", "require":
		return nil
	default:
		return fmt.Errorf("DB_SSLMODE %q is not supported (github.com/turahe/pkg/database accepts disable and require)", mode)
	}
}

func validateCredentialsSource(source string) error {
	source = strings.TrimSpace(source)
	switch {
	case source == "" || source == "workload-identity" || source == "adc":
		return nil
	case strings.HasPrefix(source, "path:"):
		if strings.TrimSpace(strings.TrimPrefix(source, "path:")) == "" {
			return errors.New("DB_GOOGLE_CREDENTIALS_SOURCE path is empty")
		}

		return nil
	default:
		return fmt.Errorf("unsupported DB_GOOGLE_CREDENTIALS_SOURCE %q", source)
	}
}

// useGoogleCredentials points Application Default Credentials at a path: file for
// the Cloud SQL dialer, which reads that variable while it is constructed.
func useGoogleCredentials(cfg config.Config) (func(), error) {
	source := strings.TrimSpace(cfg.DBGoogleCredentialsSource)
	if !cfg.UsesCloudSQL() || !strings.HasPrefix(source, "path:") {
		return func() {}, nil
	}

	path := strings.TrimSpace(strings.TrimPrefix(source, "path:"))
	if path == "" {
		return nil, errors.New("DB_GOOGLE_CREDENTIALS_SOURCE path is empty")
	}

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("DB_GOOGLE_CREDENTIALS_SOURCE credentials file: %w", err)
	}

	previous, had := os.LookupEnv("GOOGLE_APPLICATION_CREDENTIALS")
	if err := os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path); err != nil {
		return nil, err
	}

	return func() {
		if had {
			_ = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", previous)
			return
		}

		_ = os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
	}, nil
}

func loggerConfig(cfg config.Config, version string) pklogger.Config {
	level := slog.LevelInfo
	if cfg.Environment == "local" {
		level = slog.LevelDebug
	}

	return pklogger.Config{
		LogLevel:       level,
		EnableCaller:   cfg.Environment != "production",
		ServiceName:    "blog-api",
		ServiceVersion: version,
		Environment:    cfg.Environment,
		Redact:         redact,
	}
}

func redact(key string, value any) any {
	normalized := strings.ToLower(key)

	switch normalized {
	case "authorization", "cookie", "set-cookie", "dsn", "api_key", "apikey":
		return redacted
	}

	if strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		strings.HasSuffix(normalized, "token") {
		return redacted
	}

	return value
}
