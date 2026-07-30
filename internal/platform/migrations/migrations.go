package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed sql/*.sql
var files embed.FS

func Up(db *sql.DB) error {
	goose.SetBaseFS(files)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	return goose.Up(db, "sql")
}

func Down(db *sql.DB) error {
	goose.SetBaseFS(files)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	return goose.Down(db, "sql")
}

func Status(db *sql.DB) error {
	goose.SetBaseFS(files)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	return goose.Status(db, "sql")
}
