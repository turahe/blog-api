package seed

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// seedTx returns a rolled-back transaction on the migrated TEST_DATABASE_URL database.
func seedTx(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping seed test against PostgreSQL")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, migrations.Up(sqlDB))

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })

	return tx
}

func TestSeedGrantsAdminAccessToStaffRoles(t *testing.T) {
	t.Parallel()

	tx := seedTx(t)
	ctx := t.Context()
	name := "seed" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	require.NoError(t, Run(ctx, tx, Options{AdminEmail: name + "@example.test", AdminUsername: name}))
	require.NoError(t, Run(ctx, tx, Options{AdminEmail: name + "@example.test", AdminUsername: name}), "seeding is idempotent")

	enforcer, err := outboundrbac.NewEnforcer(tx)
	require.NoError(t, err)

	roles := outboundrbac.NewRoleStore(tx, enforcer)
	newUser := func(role string) uuid.UUID {
		var id uuid.UUID

		uname := role + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		require.NoError(t, tx.Raw("INSERT INTO users (email, username, full_name) VALUES (?, ?, ?) RETURNING uuid",
			uname+"@example.test", uname, uname).Row().Scan(&id))

		if role != "" {
			require.NoError(t, roles.AssignRoles(ctx, id, []string{role}))
		}

		return id
	}

	for _, role := range []string{roleAdmin, roleEditor, roleAuthor, roleModerator} {
		allowed, err := enforcer.Enforce(ctx, newUser(role), permAdminAccess)
		require.NoError(t, err)
		require.True(t, allowed, role)
	}

	allowed, err := enforcer.Enforce(ctx, newUser(""), permAdminAccess)
	require.NoError(t, err)
	require.False(t, allowed, "an account without a staff role has no admin access")
}
