package seed

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	outboundrbac "github.com/turahe/blog-api/internal/adapters/outbound/rbac"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	requireSettingsSeeded(t, tx)

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

var errInjected = errors.New("injected failure")

// failStatements makes every statement on db for which fail returns true error with
// errInjected before it reaches PostgreSQL, so the test transaction stays usable.
func failStatements(t *testing.T, db *gorm.DB, fail func(op string, stmt *gorm.Statement) bool) {
	t.Helper()

	hook := func(op string) func(*gorm.DB) {
		return func(d *gorm.DB) {
			if fail(op, d.Statement) {
				_ = d.AddError(errInjected)
			}
		}
	}

	callbacks := db.Callback()
	require.NoError(t, callbacks.Query().Before("gorm:query").Register("seedtest:query", hook("query")))
	require.NoError(t, callbacks.Create().Before("gorm:create").Register("seedtest:create", hook("create")))
	require.NoError(t, callbacks.Raw().Before("gorm:raw").Register("seedtest:raw", hook("raw")))
	require.NoError(t, callbacks.Row().Before("gorm:row").Register("seedtest:row", hook("row")))
}

// These run sequentially: Run inserts the same fixed role and permission rows, and
// concurrent seeds in separate transactions would block each other on those keys.
func TestRunReportsDatabaseFailures(t *testing.T) {
	statement := func(wantOp, table, sqlFragment string) func(string, *gorm.Statement) bool {
		return func(op string, stmt *gorm.Statement) bool {
			return op == wantOp && (table == "" || stmt.Table == table) &&
				(sqlFragment == "" || strings.Contains(stmt.SQL.String(), sqlFragment))
		}
	}

	// after fails the first statement matching fail once any statement touches trigger.
	after := func(trigger string, fail func(string, *gorm.Statement) bool) func(string, *gorm.Statement) bool {
		armed := false

		return func(op string, stmt *gorm.Statement) bool {
			if armed && fail(op, stmt) {
				return true
			}

			armed = armed || stmt.Table == trigger

			return false
		}
	}

	tests := []struct {
		name string
		fail func(op string, stmt *gorm.Statement) bool
	}{
		{name: "role lookup", fail: statement("query", "roles", "")},
		{name: "permission lookup", fail: statement("query", "permissions", "")},
		{name: "role permission mirror", fail: statement("raw", "", "INSERT INTO role_permissions")},
		{name: "casbin policy load", fail: statement("query", "casbin_rules", "")},
		{name: "casbin policy write", fail: func(op string, stmt *gorm.Statement) bool {
			if op == "query" && stmt.Table == "casbin_rules" {
				// Load no policy so every seeded grant is new and reaches the adapter.
				stmt.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "false"}}})
			}

			return op == "create" && stmt.Table == "casbin_rules"
		}},
		{name: "admin lookup", fail: statement("query", "users", "")},
		{name: "admin create", fail: statement("create", "users", "")},
		{name: "admin role lookup", fail: after("users", statement("query", "roles", ""))},
		{name: "admin role grant", fail: after("user_roles", statement("create", "casbin_rules", ""))},
		{name: "notification templates", fail: statement("create", "notification_templates", "")},
		{name: "settings", fail: statement("raw", "", "INSERT INTO settings")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := seedTx(t)
			failStatements(t, tx, tt.fail)
			name := "seed" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

			err := Run(t.Context(), tx, Options{AdminEmail: name + "@example.test", AdminUsername: name})
			require.ErrorIs(t, err, errInjected)
		})
	}
}

// requireSettingsSeeded checks every catalogue key is stored at its default with exactly
// one seed history row, however often Run ran.
func requireSettingsSeeded(t *testing.T, tx *gorm.DB) {
	t.Helper()

	for _, def := range settingsdomain.DefaultCatalogue().Definitions() {
		var value string

		require.NoError(t, tx.Raw("SELECT value::text FROM settings WHERE key = ?", def.Key).Row().Scan(&value), def.Key)

		want, err := json.Marshal(def.Default)
		require.NoError(t, err)
		require.JSONEq(t, string(want), value, def.Key)

		var seeded int64

		require.NoError(t, tx.Raw("SELECT count(*) FROM settings_history WHERE setting_key = ? AND request_id = 'seed'",
			def.Key).Row().Scan(&seeded))
		require.EqualValues(t, 1, seeded, def.Key)
	}
}
