package bootstrap

import (
	"context"
	"log/slog"

	pklogger "github.com/turahe/pkg/logger"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/connect"
	"github.com/turahe/blog-api/internal/platform/sentry"
)

// NewLogger installs github.com/turahe/pkg/logger, starts Sentry when SENTRY_DSN
// is set, and returns that logger plus a flush func to defer until shutdown.
// Error records are also sent to Sentry. Records at SENTRY_LOGS_LEVEL go to
// Sentry Logs. The flush func delivers buffered events.
func NewLogger(cfg config.Config, version string) (*slog.Logger, func(), error) {
	connect.Install(cfg, version)

	flush, err := sentry.Init(cfg, version)
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

	log := pklogger.GetLogger()
	if !cfg.SentryEnabled() {
		return log, flush, nil
	}

	handler := sentry.NewHandler()
	if level, ok := cfg.SentryLogs(); ok {
		handler = handler.WithLogs(level)
	}

	return slog.New(slog.NewMultiHandler(log.Handler(), handler)), flush, nil
}
