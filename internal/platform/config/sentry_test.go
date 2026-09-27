package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLoadSentryDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ENV", "staging")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.SentryEnabled() {
		t.Fatal("expected Sentry disabled without SENTRY_DSN")
	}

	if cfg.SentryEnvironment != "staging" {
		t.Fatalf("environment=%q, want APP_ENV fallback", cfg.SentryEnvironment)
	}

	if cfg.SentryTracesSampleRate != 0.1 {
		t.Fatalf("rate=%v", cfg.SentryTracesSampleRate)
	}

	if cfg.SentryLogsLevel != "info" {
		t.Fatalf("logs level=%q", cfg.SentryLogsLevel)
	}
}

func TestLoadSentryFromEnv(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("SENTRY_DSN", "https://public@o1.ingest.sentry.io/1")
	t.Setenv("SENTRY_ENVIRONMENT", "prod-eu")
	t.Setenv("SENTRY_TRACES_SAMPLE_RATE", "0.25")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.SentryEnabled() || cfg.SentryEnvironment != "prod-eu" || cfg.SentryTracesSampleRate != 0.25 {
		t.Fatalf("unexpected sentry config: %+v", cfg)
	}
}

func TestSentryLogsLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		dsn, raw string
		want     slog.Level
		on       bool
	}{
		{"https://public@o1.ingest.sentry.io/1", "info", slog.LevelInfo, true},
		{"https://public@o1.ingest.sentry.io/1", "WARN", slog.LevelWarn, true},
		{"https://public@o1.ingest.sentry.io/1", "off", 0, false},
		{"", "info", 0, false},
	} {
		level, on := Config{SentryDSN: tc.dsn, SentryLogsLevel: tc.raw}.SentryLogs()
		if level != tc.want || on != tc.on {
			t.Fatalf("SentryLogs(%q, dsn=%q) = %v, %v; want %v, %v", tc.raw, tc.dsn, level, on, tc.want, tc.on)
		}
	}
}

func TestLoadRejectsUnknownSentryLogsLevel(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("SENTRY_LOGS_LEVEL", "verbose")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SENTRY_LOGS_LEVEL") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadRejectsOutOfRangeSentrySampleRate(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("SENTRY_TRACES_SAMPLE_RATE", "1.5")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SENTRY_TRACES_SAMPLE_RATE") {
		t.Fatalf("err=%v", err)
	}
}
