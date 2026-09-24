package persistence

import (
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testDatabaseURLEnv names a disposable PostgreSQL database for repository tests.
// Migrations are applied to it; every test runs inside a transaction that is rolled back.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

var (
	integrationOnce sync.Once
	integrationGorm *gorm.DB
	errIntegration  error
)

// integrationTx returns a transaction on the migrated test database, rolled back at test end.
// The test is skipped when TEST_DATABASE_URL is unset.
func integrationTx(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping repository test against PostgreSQL", testDatabaseURLEnv)
	}

	integrationOnce.Do(func() {
		sqlDB, err := sql.Open("pgx", dsn)
		if err != nil {
			errIntegration = err
			return
		}

		if err = migrations.Up(sqlDB); err != nil {
			errIntegration = err
			return
		}

		integrationGorm, errIntegration = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}),
			&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	})
	require.NoError(t, errIntegration)

	tx := integrationGorm.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })

	return tx
}

// uniqueSlug returns a slug unlikely to collide with rows committed by other runs.
func uniqueSlug(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
}

func insertUUID(t *testing.T, tx *gorm.DB, query string, args ...any) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	require.NoError(t, tx.Raw(query, args...).Row().Scan(&id))

	return id
}

func insertUser(t *testing.T, tx *gorm.DB) uuid.UUID {
	t.Helper()

	name := uniqueSlug("user")

	return insertUUID(t, tx, "INSERT INTO users (email, username, full_name) VALUES (?, ?, ?) RETURNING uuid",
		name+"@example.test", name, name)
}

func insertCategory(t *testing.T, tx *gorm.DB) uuid.UUID {
	t.Helper()

	slug := uniqueSlug("cat")

	return insertUUID(t, tx, "INSERT INTO categories (name, slug) VALUES (?, ?) RETURNING uuid", slug, slug)
}

func insertTag(t *testing.T, tx *gorm.DB, postIDs ...uuid.UUID) uuid.UUID {
	t.Helper()

	slug := uniqueSlug("tag")
	tagID := insertUUID(t, tx, "INSERT INTO tags (name, slug) VALUES (?, ?) RETURNING uuid", slug, slug)

	for _, postID := range postIDs {
		require.NoError(t, tx.Exec(
			"INSERT INTO post_tags (post_id, tag_id) VALUES ("+idOf("posts")+", "+idOf("tags")+")", postID, tagID,
		).Error)
	}

	return tagID
}
