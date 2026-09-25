package bootstrap

import (
	"context"
	"log/slog"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	"github.com/turahe/blog-api/internal/core/event"
	privacyports "github.com/turahe/blog-api/internal/core/privacy/ports"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
	"github.com/turahe/blog-api/internal/platform/system"
)

// NewPrivacyService wires personal data exports and erasures. Exports are stored in the media
// bucket; without media storage, or when its client cannot be built, only erasure is available.
// verifier may be nil where requests are only processed (app scheduler).
func NewPrivacyService(
	ctx context.Context,
	cfg config.Config,
	db *database.Database,
	verifier privacyports.PasswordVerifier,
	events event.Unit,
	readCache readcache.Cache,
	logger *slog.Logger,
) *privacyservice.Service {
	data := persistence.NewPrivacyData(db.GORM)
	svc := privacyservice.New(persistence.NewPrivacyRepository(db.GORM), data, data, verifier,
		system.UUIDGenerator{}, system.Clock{}, privacyservice.Config{
			ExportRetention: cfg.PrivacyExportRetention,
			DownloadTTL:     cfg.PrivacyExportURLTTL,
		}).WithEvents(events).WithCache(readCache)

	if !cfg.MediaEnabled() {
		return svc
	}

	archives, err := storage.NewS3(ctx, cfg)
	if err != nil {
		logger.Warn("personal data exports disabled: object storage unavailable", "error", err)
		return svc
	}

	return svc.WithArchives(archives)
}
