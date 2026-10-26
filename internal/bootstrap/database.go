package bootstrap

import (
	"context"
	"fmt"

	pkgdb "github.com/turahe/pkg/database"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/connect"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// OpenDatabase opens Postgres or Cloud SQL through github.com/turahe/pkg/database
// and records GORM statements on the current Sentry span.
func OpenDatabase(ctx context.Context, cfg config.Config) (*pkgdb.Database, error) {
	db, err := connect.OpenDatabase(ctx, cfg)
	if err != nil {
		return nil, err
	}

	gdb := db.DB()
	gdb.Config.DisableForeignKeyConstraintWhenMigrating = true

	if err := gdb.Use(logging.GORMTracing{}); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("register gorm tracing: %w", err)
	}

	return db, nil
}
