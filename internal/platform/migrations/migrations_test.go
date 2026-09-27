package migrations

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// The tests in this file do not run in parallel: goose keeps its base FS, dialect, and
// logger in package globals that every call rewrites.

func testDatabaseURL(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping migration test against PostgreSQL")
	}

	goose.SetLogger(goose.NopLogger())

	return dsn
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// isolatedSchemaDB returns a pool whose search_path is only a fresh schema, dropped after the
// test. Leaving public off the path matters: goose's version table and the migrations use
// unqualified names, which would otherwise resolve to the shared test schema.
func isolatedSchemaDB(t *testing.T) (*sql.DB, string) {
	t.Helper()

	dsn := testDatabaseURL(t)
	admin := openDB(t, dsn)
	schema := "migtest_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	// Extensions are database-wide: on a fresh database the isolated Up would install pgcrypto
	// into the throwaway schema and DROP SCHEMA would remove it under concurrent packages.
	require.NoError(t, Up(admin))

	_, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(context.WithoutCancel(t.Context()), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})

	u, err := url.Parse(dsn)
	require.NoError(t, err)

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	return openDB(t, u.String()), schema
}

// requireInSchema fails the test unless the unqualified table name resolves inside schema.
func requireInSchema(t *testing.T, db *sql.DB, schema, table string) {
	t.Helper()

	var got sql.NullString
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT n.nspname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = to_regclass($1)`, table).Scan(&got))
	require.Equal(t, schema, got.String, table)
}

func TestUp(t *testing.T) {
	tests := []struct {
		name     string
		maxConns int
	}{
		{name: "with migration lock"},
		{name: "single connection pool skips the lock", maxConns: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t, testDatabaseURL(t))
			db.SetMaxOpenConns(tt.maxConns)

			require.NoError(t, Up(db))
			require.NoError(t, Up(db), "applying again is a no-op")

			var registered bool
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass('registrations') IS NOT NULL").Scan(&registered))
			require.True(t, registered)
		})
	}
}

func TestDown(t *testing.T) {
	db, schema := isolatedSchemaDB(t)
	require.NoError(t, Up(db))
	requireInSchema(t, db, schema, "goose_db_version")
	requireInSchema(t, db, schema, "registrations")

	latest, err := goose.GetDBVersion(db)
	require.NoError(t, err)

	require.NoError(t, Down(db))

	previous, err := goose.GetDBVersion(db)
	require.NoError(t, err)
	require.Less(t, previous, latest)

	require.NoError(t, Up(db), "the newest migration applies again after rolling it back")

	restored, err := goose.GetDBVersion(db)
	require.NoError(t, err)
	require.Equal(t, latest, restored)
}

func TestStatus(t *testing.T) {
	db := openDB(t, testDatabaseURL(t))
	require.NoError(t, Up(db))
	require.NoError(t, Status(db))
}

func TestClosedDatabase(t *testing.T) {
	goose.SetLogger(goose.NopLogger())

	db, err := sql.Open("pgx", "postgres://blog@127.0.0.1:1/blog")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	tests := []struct {
		name    string
		run     func(*sql.DB) error
		wantErr string
	}{
		{name: "up", run: Up, wantErr: "migration lock connection"},
		{name: "down", run: Down, wantErr: "database is closed"},
		{name: "status", run: Status, wantErr: "database is closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.run(db), tt.wantErr)
		})
	}
}
