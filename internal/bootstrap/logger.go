package bootstrap

import (
	"log/slog"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/logging"
	sentryplatform "github.com/turahe/blog-api/internal/platform/sentry"
)

// NewLogger initialises Sentry when SENTRY_DSN is set and returns the app
// logger (reporting Error-level records to Sentry, and records at
// SENTRY_LOGS_LEVEL to Sentry Logs) plus a flush func to defer until shutdown
// so buffered events are delivered.
func NewLogger(cfg config.Config, version string) (*slog.Logger, func(), error) {
	flush, err := sentryplatform.Init(cfg, version)
	if err != nil {
		return nil, nil, err
	}

	if !cfg.SentryEnabled() {
		return logging.New(cfg.Environment), flush, nil
	}

	handler := sentryplatform.NewHandler()
	if level, ok := cfg.SentryLogs(); ok {
		handler = handler.WithLogs(level)
	}

	return logging.New(cfg.Environment, handler), flush, nil
}
