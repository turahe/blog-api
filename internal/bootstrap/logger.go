package bootstrap

import (
	"log/slog"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/logging"
	sentryplatform "github.com/turahe/blog-api/internal/platform/sentry"
)

// NewLogger initialises Sentry when SENTRY_DSN is set and returns the app
// logger (reporting Error-level records to Sentry) plus a flush func to defer
// until shutdown so buffered events are delivered.
func NewLogger(cfg config.Config, version string) (*slog.Logger, func(), error) {
	flush, err := sentryplatform.Init(cfg, version)
	if err != nil {
		return nil, nil, err
	}

	if !cfg.SentryEnabled() {
		return logging.New(cfg.Environment), flush, nil
	}

	return logging.New(cfg.Environment, sentryplatform.NewHandler()), flush, nil
}
