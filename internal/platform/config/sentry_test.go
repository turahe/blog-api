package config

import (
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

func TestLoadRejectsOutOfRangeSentrySampleRate(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("SENTRY_TRACES_SAMPLE_RATE", "1.5")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SENTRY_TRACES_SAMPLE_RATE") {
		t.Fatalf("err=%v", err)
	}
}
