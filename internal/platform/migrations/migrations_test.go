package migrations_test

import (
	"bytes"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/platform/migrations"
)

func testGorm(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	db.DisableForeignKeyConstraintWhenMigrating = true

	return db
}

func TestUp(t *testing.T) {
	t.Parallel()

	db := testGorm(t)
	require.NoError(t, migrations.Up(db))
	require.True(t, db.Migrator().HasTable(&persistence.UserModel{}))
	require.True(t, db.Migrator().HasTable(&persistence.OutboxEventsSchema{}))
}

func TestStatus(t *testing.T) {
	t.Parallel()

	db := testGorm(t)
	require.NoError(t, migrations.Up(db))

	var buf bytes.Buffer
	require.NoError(t, persistence.SchemaStatusTo(db, &buf))
	require.Contains(t, buf.String(), "present\tusers")
}

func TestDown(t *testing.T) {
	t.Parallel()

	if os.Getenv("TEST_MIGRATE_DOWN") == "" {
		t.Skip("set TEST_MIGRATE_DOWN=1 to exercise destructive migrate down")
	}

	db := testGorm(t)
	require.NoError(t, migrations.Up(db))
	require.NoError(t, migrations.Down(db))
	require.False(t, db.Migrator().HasTable(&persistence.UserModel{}))
}

func TestClosedDatabase(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("pgx", "postgres://blog@127.0.0.1:1/blog")
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		require.ErrorContains(t, err, "closed")
		return
	}

	err = migrations.Up(db)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "connect") ||
		strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "automigrate"))
}
