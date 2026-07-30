// Package postgres is a compatibility shim. Prefer internal/platform/database.
package postgres

import (
	"context"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

// Database is an alias for the multi-dialect platform Database.
type Database = database.Database

// Open connects using the multi-dialect platform opener (postgres/mysql/sqlserver + Cloud SQL).
func Open(ctx context.Context, cfg config.Config) (*Database, error) {
	return database.Open(ctx, cfg)
}
