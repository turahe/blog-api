package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadExtendsPkgConfig(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_KEY", strings.Repeat("k", 32))
	t.Setenv("APP_ADDR", "0.0.0.0:9090")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_NAME", "blog")
	t.Setenv("DB_USER", "blog")
	t.Setenv("DB_SSLMODE", "require")
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("APP_JWT_ISSUER", "blog-api")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Port != "9090" || cfg.Server.Mode != "release" || cfg.Server.JWTSigningAlgorithm != "ES256" {
		t.Fatalf("server port=%s mode=%s alg=%s", cfg.Server.Port, cfg.Server.Mode, cfg.Server.JWTSigningAlgorithm)
	}

	if string(cfg.Server.JWTPrivateKeyPEM) != cfg.JWTPrivateKey || cfg.Server.JWTIssuer != "blog-api" {
		t.Fatal("JWT settings were not projected onto turahe/pkg config")
	}

	if cfg.Database.Host != "db.internal" || cfg.Database.Dbname != "blog" || !cfg.Database.Sslmode || cfg.Database.Driver != DBDriverPostgres {
		t.Fatalf("database host=%s name=%s ssl=%v driver=%s", cfg.Database.Host, cfg.Database.Dbname, cfg.Database.Sslmode, cfg.Database.Driver)
	}

	if !cfg.Redis.Enabled || cfg.Redis.Host != "redis.internal" {
		t.Fatalf("redis=%+v", cfg.Redis)
	}

	shared := cfg.PkgConfig("v9")
	if shared.Sentry.Release != "v9" || shared.OpenTelemetry.ServiceVersion != "v9" {
		t.Fatalf("release projection sentry=%s otel=%s", shared.Sentry.Release, shared.OpenTelemetry.ServiceVersion)
	}
}

func TestLoadReadsPkgEnv(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ADDR", "")
	t.Setenv("APP_ACCESS_TOKEN_TTL", "")
	t.Setenv("DATABASE_HOST", "pkg-db")
	t.Setenv("DB_HOST", "legacy-db")
	t.Setenv("DATABASE_DBNAME", "pkgblog")
	t.Setenv("DATABASE_USERNAME", "pkguser")
	t.Setenv("DATABASE_SSLMODE", "true")
	t.Setenv("DATABASE_CLOUD_SQL_INSTANCE", "")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "40")
	t.Setenv("DATABASE_CONN_MAX_LIFETIME", "15")
	t.Setenv("DATABASE_TIMEZONE", "Asia/Jakarta")
	t.Setenv("SERVER_PORT", "9091")
	t.Setenv("SERVER_MODE", "release")
	t.Setenv("SERVER_TIMEZONE", "Asia/Jakarta")
	t.Setenv("SERVER_ACCESS_TOKEN_EXPIRY", "2")
	t.Setenv("JWT_ISSUER", "pkg-issuer")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DBHost != "pkg-db" || cfg.DBName != "pkgblog" || cfg.DBUser != "pkguser" || cfg.DBSSLMode != "require" {
		t.Fatalf("database host=%s name=%s user=%s ssl=%s", cfg.DBHost, cfg.DBName, cfg.DBUser, cfg.DBSSLMode)
	}

	if cfg.DBMaxOpen != 40 || cfg.DBMaxLifetime != 15*time.Minute {
		t.Fatalf("pool open=%d lifetime=%s", cfg.DBMaxOpen, cfg.DBMaxLifetime)
	}

	if cfg.Address != "0.0.0.0:9091" || cfg.Server.Port != "9091" || cfg.Server.Mode != "release" {
		t.Fatalf("listen=%s port=%s mode=%s", cfg.Address, cfg.Server.Port, cfg.Server.Mode)
	}

	if cfg.Timezone.Timezone != "Asia/Jakarta" || cfg.Database.ConnectionTimezone != "Asia/Jakarta" {
		t.Fatalf("tz server=%s db=%s", cfg.Timezone.Timezone, cfg.Database.ConnectionTimezone)
	}

	if cfg.AccessTokenTTL != 2*time.Hour || cfg.JWTIssuer != "pkg-issuer" || cfg.Server.JWTIssuer != "pkg-issuer" {
		t.Fatalf("ttl=%s issuer=%s", cfg.AccessTokenTTL, cfg.JWTIssuer)
	}

	if cfg.OpenTelemetry.Endpoint != "localhost:4318" || cfg.OpenTelemetry.Protocol != "grpc" {
		t.Fatalf("otel endpoint=%s protocol=%s", cfg.OpenTelemetry.Endpoint, cfg.OpenTelemetry.Protocol)
	}
}
