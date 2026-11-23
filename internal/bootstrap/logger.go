package bootstrap

import (
	"context"
	"log/slog"
	"time"

	pklogger "github.com/turahe/pkg/logger"
	"github.com/turahe/pkg/sentryx"
	pkgstorage "github.com/turahe/pkg/storage"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/connect"
)

// NewLogger installs github.com/turahe/pkg (logger, crypto, optional storage),
// starts Sentry through github.com/turahe/pkg/sentryx when SENTRY_DSN is set, and
// returns that logger plus a flush func to defer until shutdown. An invalid DSN is
// logged and Sentry stays off; startup continues. The flush func delivers buffered
// events and closes pkg storage.
func NewLogger(cfg config.Config, version string) (*slog.Logger, func(), error) {
	connect.Install(cfg, version)

	_ = sentryx.Init(connect.SentryConfig(cfg, version))

	// Process start, before any request context exists.
	stopTracing := connect.InitTracing(context.Background(), cfg, version)
	flush := func() {
		sentryx.Flush(2 * time.Second)
		_ = stopTracing(context.Background())
		_ = pkgstorage.Close()
	}

	return pklogger.GetLogger(), flush, nil
}
