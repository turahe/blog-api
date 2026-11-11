package bootstrap

import (
	"context"

	pkgdb "github.com/turahe/pkg/database"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/connect"
)

// OpenDatabase opens Postgres or Cloud SQL through github.com/turahe/pkg/database.
func OpenDatabase(ctx context.Context, cfg config.Config) (*pkgdb.Database, error) {
	db, err := connect.OpenDatabase(ctx, cfg)
	if err != nil {
		return nil, err
	}

	db.DB().Config.DisableForeignKeyConstraintWhenMigrating = true

	return db, nil
}
