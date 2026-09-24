// Package postgres is a compatibility shim. Prefer internal/platform/database.
package postgres

import (
	"context"

	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

// Database is an alias for the platform Database.
type Database = database.Database

// Open connects using the platform opener (direct PostgreSQL or Cloud SQL).
func Open(ctx context.Context, cfg config.Config) (*Database, error) {
	return database.Open(ctx, cfg)
}
