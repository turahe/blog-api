package rbac

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	"github.com/turahe/blog-api/internal/platform/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	integrationOnce sync.Once
	integrationGorm *gorm.DB
	errIntegration  error
)

// integrationTx returns a transaction on the migrated TEST_DATABASE_URL database,
// rolled back at test end. The test is skipped when the variable is unset.
func integrationTx(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping role store test against PostgreSQL")
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

func suffix() string { return strings.ReplaceAll(uuid.NewString()[:8], "-", "") }

// The peer reloads concurrently with the writer, which a single transaction's
// connection cannot serve, so this test commits its rows and deletes them after.
func TestPolicySyncPropagatesWritesToPeers(t *testing.T) {
	integrationTx(t)

	db := integrationGorm
	ctx := context.Background()

	perm := "itest.sync." + suffix()
	name := "itest_" + suffix()
	user := uuid.New()

	t.Cleanup(func() {
		db.Exec("DELETE FROM casbin_rules WHERE v0 IN (?, ?) OR v1 = ?", name, user.String(), name)
		db.Exec("DELETE FROM roles WHERE name = ?", name)
		db.Exec("DELETE FROM users WHERE uuid = ?", user)
		db.Exec("DELETE FROM permissions WHERE key = ?", perm)
	})

	require.NoError(t, db.Exec("INSERT INTO permissions (key) VALUES (?)", perm).Error)
	require.NoError(t, db.Exec("INSERT INTO users (uuid, email, username, full_name) VALUES (?, ?, ?, ?)",
		user, name+"@example.test", name, name).Error)

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writer, err := NewEnforcer(db)
	require.NoError(t, err)

	peer, err := NewEnforcer(db)
	require.NoError(t, err)

	peerSync := NewPolicySync(peer, client, 0, logger)
	peerSync.Start(ctx)
	t.Cleanup(peerSync.Stop)

	require.Eventually(t, func() bool { return mr.PubSubNumSub(PolicyChannel)[PolicyChannel] > 0 }, 2*time.Second, 10*time.Millisecond)

	store := NewRoleStore(db, writer).WithNotifier(NewPolicySync(writer, client, 0, logger))
	_, err = store.CreateRole(ctx, rbacdomain.Role{Name: name, Permissions: []string{perm}})
	require.NoError(t, err)
	require.NoError(t, store.AssignRoles(ctx, user, []string{name}))

	require.Eventually(t, func() bool {
		ok, err := peer.Enforce(ctx, user, perm)
		return err == nil && ok
	}, 2*time.Second, 10*time.Millisecond, "the peer reloads after the announcement")
}

func TestRoleStoreLifecycleSyncsEnforcer(t *testing.T) {
	tx := integrationTx(t)
	ctx := context.Background()

	perm := "itest.read." + suffix()
	require.NoError(t, tx.Exec("INSERT INTO permissions (key) VALUES (?)", perm).Error)

	name := "itest_" + suffix()
	user := uuid.New()
	require.NoError(t, tx.Exec("INSERT INTO users (uuid, email, username, full_name) VALUES (?, ?, ?, ?)",
		user, name+"@example.test", name, name).Error)

	enforcer, err := NewEnforcer(tx)
	require.NoError(t, err)

	store := NewRoleStore(tx, enforcer)

	role, err := store.CreateRole(ctx, rbacdomain.Role{Name: name, Description: "d", Permissions: []string{perm}})
	require.NoError(t, err)
	require.Equal(t, []string{perm}, role.Permissions)

	_, err = store.CreateRole(ctx, rbacdomain.Role{Name: name})
	require.ErrorIs(t, err, rbacdomain.ErrRoleExists)

	require.NoError(t, store.AssignRoles(ctx, user, []string{name}))
	require.NoError(t, store.AssignRoles(ctx, user, []string{name}), "assigning twice is a no-op")

	names, err := store.UserRoles(ctx, user)
	require.NoError(t, err)
	require.Equal(t, []string{name}, names)

	allowed, err := enforcer.Enforce(ctx, user, perm)
	require.NoError(t, err)
	require.True(t, allowed, "the enforcer sees the grant without a restart")

	role, err = store.SetRolePermissions(ctx, name, nil)
	require.NoError(t, err)
	require.Empty(t, role.Permissions)

	allowed, err = enforcer.Enforce(ctx, user, perm)
	require.NoError(t, err)
	require.False(t, allowed)

	require.NoError(t, store.RevokeRole(ctx, user, name))

	names, err = store.UserRoles(ctx, user)
	require.NoError(t, err)
	require.Empty(t, names)

	require.NoError(t, store.DeleteRole(ctx, name))

	_, err = store.FindRole(ctx, name)
	require.ErrorIs(t, err, rbacdomain.ErrRoleNotFound)

	require.ErrorIs(t, store.CheckPermissions(ctx, []string{"itest.missing." + suffix()}), rbacdomain.ErrPermissionNotFound)

	_, err = store.UserRoles(ctx, uuid.New())
	require.ErrorIs(t, err, rbacdomain.ErrUserNotFound)
}
