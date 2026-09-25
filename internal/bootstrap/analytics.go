package bootstrap

import (
	"log/slog"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/system"
)

// newAnalytics starts the background writer for raw analytics events and returns the ingest
// service that feeds it. Visitor hashes are keyed with APP_ENCRYPTION_KEY when it is set.
func newAnalytics(
	cfg config.Config, db *database.Database, onDrop func(int), logger *slog.Logger,
) (*analyticsservice.Ingest, *analyticsservice.Writer) {
	writer := analyticsservice.NewWriter(persistence.NewAnalyticsRepository(db.GORM), logger, analyticsservice.WriterOptions{
		QueueSize: cfg.AnalyticsQueueSize,
		OnDrop:    onDrop,
	})

	return analyticsservice.NewIngest(writer, identityHasher(cfg), system.UUIDGenerator{}, system.Clock{}), writer
}
