// Package migrations exposes CLI-facing schema commands backed by GORM AutoMigrate.
package migrations

import (
	"gorm.io/gorm"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
)

// Up creates or updates the application schema.
func Up(db *gorm.DB) error { return persistence.Migrate(db) }

// Down drops AutoMigrate tables (destructive; local/dev).
func Down(db *gorm.DB) error { return persistence.DropAll(db) }

// Status prints which schema tables are present.
func Status(db *gorm.DB) error { return persistence.SchemaStatus(db) }
