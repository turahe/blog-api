package bootstrap

import (
	"context"
	"log/slog"

	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	"github.com/turahe/blog-api/internal/adapters/outbound/analyticslive"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	analyticsports "github.com/turahe/blog-api/internal/core/analytics/ports"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	settingsservice "github.com/turahe/blog-api/internal/core/settings/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/messaging"
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

// newAnalyticsLive feeds this replica's live view from the broadcast bus and has ingest announce
// accepted events on it. Without a bus, or when it cannot subscribe, it returns nils and the
// live stream answers 503.
func newAnalyticsLive(
	ctx context.Context, cfg config.Config, bus *messaging.Bus, ingest *analyticsservice.Ingest, logger *slog.Logger,
) (*analyticsservice.Board, *realtime.Hub) {
	if bus == nil {
		return nil, nil
	}

	topic := bus.Topic(analyticslive.Topic)
	board := analyticsservice.NewBoard(system.Clock{})

	if err := analyticslive.Consume(ctx, bus.Subscriber, topic, board.Add, logger); err != nil {
		logger.Warn("live analytics disabled: cannot subscribe", "error", err)
		return nil, nil
	}

	publisher := analyticslive.NewPublisher(bus.Publisher, topic, logger)
	go publisher.Run(ctx, analyticslive.FlushEvery)

	ingest.WithLive(publisher)

	return board, realtime.NewHub(cfg.SSEMaxConcurrentPerUser, 1)
}

// NewAnalyticsAggregator returns the rollup builder used by app scheduler and app analytics.
// Rollups are bucketed in the site.timezone setting.
func NewAnalyticsAggregator(db *database.Database) *analyticsservice.Aggregator {
	return analyticsservice.NewAggregator(persistence.NewAnalyticsRollupRepository(db.GORM), siteTimezone{newSettingsReader(db)},
		system.Clock{})
}

// NewAnalyticsExports wires rollup exports. Archives are stored in the media bucket; without
// media storage, or when its client cannot be built, requests answer 503. stepUp may be nil
// where exports are only built (app scheduler).
func NewAnalyticsExports(
	ctx context.Context, cfg config.Config, db *database.Database, stepUp analyticsports.StepUp, logger *slog.Logger,
) *analyticsservice.Exports {
	repo := persistence.NewAnalyticsExportRepository(db.GORM)

	svc := analyticsservice.NewExports(repo, repo, siteTimezone{newSettingsReader(db)}, system.UUIDGenerator{}, system.Clock{},
		analyticsservice.ExportConfig{Retention: cfg.AnalyticsExportRetention, DownloadTTL: cfg.AnalyticsExportURLTTL})
	if stepUp != nil {
		svc.WithStepUp(stepUp)
	}

	if !cfg.MediaEnabled() {
		return svc
	}

	archives, err := storage.NewS3(ctx, cfg)
	if err != nil {
		logger.Warn("analytics exports disabled: object storage unavailable", "error", err)
		return svc
	}

	return svc.WithArchives(archives)
}

// NewAnalyticsRetention returns the pruning job of raw events and daily rollups, configured by
// the analytics.raw_retention_days and analytics.rollup_day_retention_months settings.
func NewAnalyticsRetention(db *database.Database) *analyticsservice.Retention {
	settings := newSettingsReader(db)

	return analyticsservice.NewRetention(persistence.NewAnalyticsRetentionRepository(db.GORM), retentionSettings{settings},
		siteTimezone{settings}, system.Clock{})
}

func newSettingsReader(db *database.Database) *settingsservice.Service {
	return settingsservice.New(persistence.NewSettingsRepository(db.GORM), settingsdomain.DefaultCatalogue(),
		system.UUIDGenerator{}, system.Clock{})
}

// newAnalyticsReports returns the admin dashboard reports, read from rollups in the site time zone.
func newAnalyticsReports(db *database.Database, settings *settingsservice.Service) *analyticsservice.Reports {
	return analyticsservice.NewReports(persistence.NewAnalyticsReportRepository(db.GORM), siteTimezone{settings}, system.Clock{})
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

type retentionSettings struct {
	settings *settingsservice.Service
}

func (s retentionSettings) RawRetentionDays(ctx context.Context) (int, error) {
	return s.integer(ctx, "analytics.raw_retention_days")
}

func (s retentionSettings) DayRollupRetentionMonths(ctx context.Context) (int, error) {
	return s.integer(ctx, "analytics.rollup_day_retention_months")
}

func (s retentionSettings) integer(ctx context.Context, key string) (int, error) {
	values, err := s.settings.Values(ctx)
	if err != nil {
		return 0, err
	}

	return int(values.Int(key)), nil
}
