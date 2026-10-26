package bootstrap

import (
	"context"
	"fmt"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/adapters/outbound/storage"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/system"
	pkgdb "github.com/turahe/pkg/database"
)

// NewMediaJanitor returns the media service for maintenance jobs such as the orphan cleanup,
// or nil when media storage is not configured.
func NewMediaJanitor(ctx context.Context, cfg config.Config, db *pkgdb.Database) (*mediaservice.Service, error) {
	if !cfg.MediaEnabled() {
		return nil, nil
	}

	objectStorage, err := storage.NewS3(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create media storage: %w", err)
	}

	return mediaservice.New(
		persistence.NewMediaRepository(db.DB()), objectStorage, system.UUIDGenerator{}, system.Clock{},
		cfg.S3Disk, cfg.MediaAllowedMIMETypes, cfg.MediaMaxUploadBytes, cfg.MediaPresignTTL,
	), nil
}
