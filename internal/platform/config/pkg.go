package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	pkgconfig "github.com/turahe/pkg/config"
)

// PkgConfig projects this API's settings onto the embedded turahe/pkg configuration.
// version is the process release (Sentry and OpenTelemetry); pass "" at load time.
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
			JWTSigningAlgorithm: "ES256",
			JWTPrivateKeyPEM:    []byte(c.JWTPrivateKey),
			JWTPublicKeyPEM:     []byte(c.JWTPublicKey),
			JWTIssuer:           c.JWTIssuer,
			JWTAudience:         os.Getenv("JWT_AUDIENCE"),
			JWTKeyID:            os.Getenv("JWT_KEY_ID"),
		},
		Database: pkgconfig.DatabaseConfiguration{
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
		},
		Redis: pkgconfig.RedisConfiguration{
			Enabled:  true,
			Host:     c.RedisHost,
			Port:     strconv.Itoa(c.RedisPort),
			Password: c.RedisPassword,
			DB:       c.RedisDB,
		},
		Sentry:        c.sentryConfig(version),
		OpenTelemetry: openTelemetryConfig(c.Environment, version),
		Timezone:      pkgconfig.TimezoneConfiguration{Timezone: envOr("SERVER_TIMEZONE", "UTC")},
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

	project := os.Getenv("OTEL_GCP_PROJECT_ID")
	if project == "" {
		project = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	insecure := true
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")), "false") {
		insecure = false
	}

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
		GCPProjectID:     project,
		GORMEnabled:      !strings.EqualFold(os.Getenv("OTEL_GORM_ENABLED"), "false"),
		GCPPropagator:    strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_GCP_PROPAGATOR")), "true"),
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
	for _, pair := range strings.Split(raw, ",") {
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
