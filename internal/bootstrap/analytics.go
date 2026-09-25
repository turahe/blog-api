package bootstrap

import (
	"context"
	"log/slog"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
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

// NewAnalyticsAggregator returns the rollup builder used by app scheduler and app analytics.
// Rollups are bucketed in the site.timezone setting.
func NewAnalyticsAggregator(db *database.Database) *analyticsservice.Aggregator {
	settings := settingsservice.New(persistence.NewSettingsRepository(db.GORM), settingsdomain.DefaultCatalogue(),
		system.UUIDGenerator{}, system.Clock{})

	return analyticsservice.NewAggregator(persistence.NewAnalyticsRollupRepository(db.GORM), siteTimezone{settings}, system.Clock{})
}

type siteTimezone struct {
	settings *settingsservice.Service
}

func (s siteTimezone) Timezone(ctx context.Context) (string, error) {
	values, err := s.settings.Values(ctx)
	if err != nil {
		return "", err
	}

	return values.String("site.timezone"), nil
}
