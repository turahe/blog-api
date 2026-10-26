package bootstrap

import (
	"context"
	"log/slog"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/connect"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// NewLogger initialises Sentry when SENTRY_DSN is set and returns the app
// logger (reporting Error-level records to Sentry, and records at
// SENTRY_LOGS_LEVEL to Sentry Logs) plus a flush func to defer until shutdown
// so buffered events are delivered.
func NewLogger(cfg config.Config, version string) (*slog.Logger, func(), error) {
	connect.Install(cfg, version)

	flush, err := logging.InitSentry(cfg, version)
	if err != nil {
		return nil, nil, err
	}

	// Process start, before any request context exists.
	stopTracing := connect.InitTracing(context.Background(), cfg, version)
	sentryFlush := flush
	flush = func() {
		sentryFlush()
		_ = stopTracing(context.Background())
	}

	if !cfg.SentryEnabled() {
		return logging.New(cfg.Environment), flush, nil
	}

	handler := logging.NewSentryHandler()
	if level, ok := cfg.SentryLogs(); ok {
		handler = handler.WithLogs(level)
	}

	return logging.New(cfg.Environment, handler), flush, nil
}
