package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	pkgconfig "github.com/turahe/pkg/config"
)

// PkgConfig projects this API's settings onto the full turahe/pkg Configuration
// (server, CORS, database, Redis, storage, rate limiter, timezone, OpenTelemetry,
// Sentry, mTLS, hashing). version is the process release (Sentry and OpenTelemetry);
// pass "" at load time.
func (c Config) PkgConfig(version string) pkgconfig.Configuration {
	port := c.DBPort
	if port == 0 {
		port = defaultDBPort
	}

	driver := defaultDBDriver
	if c.UsesCloudSQL() {
		driver = "cloudsql-postgres"
	}

	listenPort := strings.TrimSpace(os.Getenv("SERVER_PORT"))
	if listenPort == "" {
		_, parsed, err := net.SplitHostPort(c.Address)
		if err != nil || parsed == "" {
			parsed = "8080"
		}

		listenPort = parsed
	}

	return pkgconfig.Configuration{
		Server: pkgconfig.ServerConfiguration{
			Port:                listenPort,
			Mode:                serverMode(c.Environment),
			AccessTokenExpiry:   wholeHours(c.AccessTokenTTL),
			RefreshTokenExpiry:  wholeDays(c.RefreshTokenTTL),
			SessionExpiry:       integer("SERVER_SESSION_EXPIRY", 24),
			SessionCookieName:   envOr("SERVER_SESSION_COOKIE_NAME", "session"),
			SessionSecure:       boolEnv("SERVER_SESSION_SECURE", c.Environment == envProduction),
			SessionHttpOnly:     boolEnv("SERVER_SESSION_HTTP_ONLY", true),
			SessionSameSite:     envOr("SERVER_SESSION_SAME_SITE", "lax"),
			JWTSigningAlgorithm: "ES256",
			JWTPrivateKeyPEM:    []byte(c.JWTPrivateKey),
			JWTPublicKeyPEM:     []byte(c.JWTPublicKey),
			JWTIssuer:           c.JWTIssuer,
			JWTAudience:         os.Getenv("JWT_AUDIENCE"),
			JWTKeyID:            os.Getenv("JWT_KEY_ID"),
		},
		Cors:     corsConfig(c.CORSOrigins),
		Database: c.databaseConfig(driver, port),
		Redis: pkgconfig.RedisConfiguration{
			Enabled:         true,
			Host:            c.RedisHost,
			Port:            strconv.Itoa(c.RedisPort),
			Password:        c.RedisPassword,
			DB:              c.RedisDB,
			ClusterMode:     boolEnv("REDIS_CLUSTER_MODE", false),
			ClusterNodes:    os.Getenv("REDIS_CLUSTER_NODES"),
			PoolSize:        integer("REDIS_POOL_SIZE", 0),
			MinIdleConns:    integer("REDIS_MIN_IDLE_CONNS", 0),
			ReadTimeoutSec:  integer("REDIS_READ_TIMEOUT_SEC", 0),
			WriteTimeoutSec: integer("REDIS_WRITE_TIMEOUT_SEC", 0),
		},
		Storage:       storageConfig(c),
		RateLimiter:   rateLimiterConfig(),
		Timezone:      pkgconfig.TimezoneConfiguration{Timezone: envOr("SERVER_TIMEZONE", "UTC")},
		OpenTelemetry: openTelemetryConfig(c.Environment, version),
		Sentry:        c.sentryConfig(version),
		MTLS:          mtlsConfig(),
		Hashing:       hashingConfig(),
	}
}

func (c Config) databaseConfig(driver string, port int) pkgconfig.DatabaseConfiguration {
	return pkgconfig.DatabaseConfiguration{
		Driver:                 driver,
		Dbname:                 c.DBName,
		Username:               c.DBUser,
		Password:               c.DBPassword,
		Host:                   c.DBHost,
		Port:                   strconv.Itoa(port),
		Sslmode:                strings.EqualFold(strings.TrimSpace(c.DBSSLMode), "require"),
		CloudSQLInstance:       c.DBInstanceConnectionName,
		MaxIdleConns:           c.DBMaxIdle,
		MaxOpenConns:           c.DBMaxOpen,
		ConnMaxLifetimeMinutes: int(c.DBMaxLifetime / time.Minute),
		ConnectionTimezone:     strings.TrimSpace(os.Getenv("DATABASE_TIMEZONE")),
	}
}

func corsConfig(origins []string) pkgconfig.CorsConfiguration {
	cleaned := make([]string, 0, len(origins))
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			cleaned = append(cleaned, origin)
		}
	}

	if boolEnv("CORS_GLOBAL", false) || (len(cleaned) == 1 && cleaned[0] == "*") {
		return pkgconfig.CorsConfiguration{Global: true}
	}

	if len(cleaned) == 0 {
		return pkgconfig.CorsConfiguration{Global: false}
	}

	return pkgconfig.CorsConfiguration{
		Global:   false,
		Frontend: cleaned[0],
		Ips:      strings.Join(cleaned, ","),
	}
}

func storageConfig(c Config) pkgconfig.StorageConfiguration {
	if !c.MediaEnabled() {
		return pkgconfig.StorageConfiguration{}
	}

	driver := "s3"

	switch strings.ToLower(strings.TrimSpace(c.S3Disk)) {
	case "gcs", "google":
		driver = "gcs"
	case "r2", "minio", "s3", "":
		driver = "s3"
	}

	force := c.S3ForcePathStyle

	return pkgconfig.StorageConfiguration{
		Driver:          driver,
		Bucket:          c.S3Bucket,
		Endpoint:        c.S3Endpoint,
		Region:          c.S3Region,
		AccessKey:       c.S3AccessKey,
		SecretKey:       c.S3SecretKey,
		ForcePathStyle:  &force,
		CredentialsFile: strings.TrimSpace(os.Getenv("GCS_CREDENTIALS_FILE")),
	}
}

func rateLimiterConfig() pkgconfig.RateLimiterConfiguration {
	return pkgconfig.RateLimiterConfiguration{
		Enabled:    boolEnv("RATE_LIMITER_ENABLED", false),
		Requests:   integer("RATE_LIMITER_REQUESTS", 100),
		Window:     integer("RATE_LIMITER_WINDOW", 60),
		KeyBy:      envOr("RATE_LIMITER_KEY_BY", "ip"),
		SkipPaths:  envOr("RATE_LIMITER_SKIP_PATHS", "/health/live,/health/ready,/health/version,/metrics"),
		TrustProxy: boolEnv("RATE_LIMITER_TRUST_PROXY", false),
	}
}

func mtlsConfig() pkgconfig.MTLSConfiguration {
	return pkgconfig.MTLSConfiguration{
		Enabled:        boolEnv("MTLS_ENABLED", false),
		CACertFile:     envOr("MTLS_CA_CERT", "/etc/mtls/ca.crt"),
		ServerCertFile: envOr("MTLS_SERVER_CERT", "/etc/mtls/server.crt"),
		ServerKeyFile:  envOr("MTLS_SERVER_KEY", "/etc/mtls/server.key"),
		ClientCertFile: envOr("MTLS_CLIENT_CERT", "/etc/mtls/gateway.crt"),
		ClientKeyFile:  envOr("MTLS_CLIENT_KEY", "/etc/mtls/gateway.key"),
		SkipPaths:      envOr("MTLS_SKIP_PATHS", "/health/live,/health/ready,/health/version,/metrics"),
	}
}

func hashingConfig() pkgconfig.HashingConfiguration {
	// Defaults match internal/platform/security/password issueParams (argon2id t=3, 64 MiB, p=2).
	return pkgconfig.HashingConfiguration{
		Driver:       envOr("HASH_DRIVER", "argon2id"),
		BcryptRounds: integer("HASH_BCRYPT_ROUNDS", 0),
		ArgonMemory:  integer("HASH_ARGON_MEMORY", 64*1024),
		ArgonTime:    integer("HASH_ARGON_TIME", 3),
		ArgonThreads: integer("HASH_ARGON_THREADS", 2),
	}
}

func (c Config) sentryConfig(version string) pkgconfig.SentryConfiguration {
	environment := c.SentryEnvironment
	if environment == "" {
		environment = c.Environment
	}

	return pkgconfig.SentryConfiguration{
		DSN:              c.SentryDSN,
		Environment:      environment,
		Release:          version,
		ServerName:       "blog-api",
		AttachStacktrace: true,
		SampleRate:       1,
		TracesSampleRate: c.SentryTracesSampleRate,
		FlushTimeout:     2 * time.Second,
	}
}

func openTelemetryConfig(environment, version string) pkgconfig.OpenTelemetryConfiguration {
	sampler := 1.0

	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			sampler = parsed
		}
	}

	insecure := !strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")), "false")

	protocol := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"))
	if protocol == "" {
		protocol = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}

	return pkgconfig.OpenTelemetryConfiguration{
		Exporter:         os.Getenv("OTEL_TRACES_EXPORTER"),
		Protocol:         protocol,
		Endpoint:         os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		TracesEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"),
		Insecure:         insecure,
		Headers:          otelHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		ServiceName:      envOr("OTEL_SERVICE_NAME", "blog-api"),
		Environment:      envOr("OTEL_ENVIRONMENT", environment),
		ServiceVersion:   envOr("OTEL_SERVICE_VERSION", version),
		TracesSamplerArg: sampler,
		ShutdownTimeout:  otelShutdownTimeout(),
		GORMEnabled:      !strings.EqualFold(os.Getenv("OTEL_GORM_ENABLED"), "false"),
	}
}

func otelShutdownTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OTEL_SHUTDOWN_TIMEOUT"))
	if raw == "" {
		return 5 * time.Second
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 5 * time.Second
	}

	return parsed
}

func otelHeaders(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	headers := make(map[string]string)

	for pair := range strings.SplitSeq(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		key = strings.TrimSpace(key)

		value = strings.TrimSpace(value)
		if ok && key != "" {
			headers[key] = value
		}
	}

	if len(headers) == 0 {
		return nil
	}

	return headers
}

func serverMode(environment string) string {
	switch mode := strings.ToLower(strings.TrimSpace(os.Getenv("SERVER_MODE"))); mode {
	case "debug", "release", "test":
		return mode
	}

	switch environment {
	case envProduction:
		return "release"
	case "test":
		return "test"
	default:
		return "debug"
	}
}

func wholeHours(d time.Duration) int {
	if d < time.Hour {
		return 0
	}

	return int(d / time.Hour)
}

func wholeDays(d time.Duration) int {
	if d < 24*time.Hour {
		return 0
	}

	return int(d / (24 * time.Hour))
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}
