// Package migrations runs the embedded goose SQL migrations.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed sql/*.sql
var files embed.FS

// upLockKey is the pg_advisory_lock key that serializes concurrent Up calls.
const upLockKey int64 = 0x626c6f675f6d6967

// Up applies all pending migrations. A session advisory lock serializes
// concurrent callers (replicas starting together, parallel test packages).
func Up(db *sql.DB) error {
	goose.SetBaseFS(files)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	if db.Stats().MaxOpenConnections == 1 { // the lock would hold goose's only connection
		return goose.Up(db, "sql")
	}

	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration lock connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", upLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", upLockKey) //nolint:errcheck // released with the session anyway

	return goose.Up(db, "sql")
}

// Down rolls back the most recent migration.
func Down(db *sql.DB) error {
	goose.SetBaseFS(files)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	return goose.Down(db, "sql")
}

// Status logs the applied state of every migration.
func Status(db *sql.DB) error {
	goose.SetBaseFS(files)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	return goose.Status(db, "sql")
}
