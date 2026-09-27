package database

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

// testDBConfig splits TEST_DATABASE_URL into the DB_* settings Open reads.
func testDBConfig(t *testing.T) config.Config {
	t.Helper()

	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database test against PostgreSQL")
	}

	u, err := url.Parse(raw)
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	password, _ := u.User.Password()

	return config.Config{
		DBDriver: "pg", DBHost: u.Hostname(), DBPort: port, DBUser: u.User.Username(), DBPassword: password,
		DBName: strings.TrimPrefix(u.Path, "/"), DBSSLMode: u.Query().Get("sslmode"),
		DBMaxOpen: 3, DBMaxIdle: 1, DBMaxLifetime: time.Minute, DBMaxIdleTime: time.Minute,
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	cfg := testDBConfig(t)

	db, err := Open(t.Context(), cfg)
	require.NoError(t, err)

	require.Equal(t, DriverPostgres, db.Driver)
	require.Equal(t, cfg.DBMaxOpen, db.SQL.Stats().MaxOpenConnections)

	var one int
	require.NoError(t, db.GORM.WithContext(t.Context()).Raw("SELECT 1").Scan(&one).Error)
	require.Equal(t, 1, one)

	require.NoError(t, db.Close())
	require.Error(t, db.SQL.PingContext(t.Context()), "Close releases the pool")
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{name: "unsupported driver", cfg: config.Config{DBDriver: "mysql"}, wantErr: "unsupported DB_DRIVER"},
		{
			name:    "unreachable server",
			cfg:     config.Config{DBHost: "127.0.0.1", DBPort: 1, DBUser: "blog", DBName: "blog", DBMaxOpen: 1},
			wantErr: "ping postgres",
		},
		{
			name:    "incomplete cloud sql settings",
			cfg:     config.Config{DBInstanceConnectionName: "project:region:instance", DBUser: "blog"},
			wantErr: "DB_NAME is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := Open(t.Context(), tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, db)
		})
	}
}

func TestDatabaseClose(t *testing.T) {
	t.Parallel()

	errDialer := errors.New("dialer stuck")

	tests := []struct {
		name    string
		db      *Database
		wantErr error
	}{
		{name: "nil database", db: nil},
		{name: "nothing to release", db: &Database{}},
		{name: "cleanup succeeds", db: &Database{cleanup: func() error { return nil }}},
		{name: "cleanup fails", db: &Database{cleanup: func() error { return errDialer }}, wantErr: errDialer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.db.Close()
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			require.ErrorContains(t, err, "close cloud sql dialer")
		})
	}
}

func TestNormalizeDriver(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "postgres", "PostgreSQL", "pg"} {
		got, err := NormalizeDriver(in)
		if err != nil {
			t.Fatalf("NormalizeDriver(%q): %v", in, err)
		}

		if got != DriverPostgres {
			t.Fatalf("NormalizeDriver(%q)=%q want %q", in, got, DriverPostgres)
		}
	}

	for _, in := range []string{"mysql", "mariadb", "sqlserver", "mssql", "oracle"} {
		if _, err := NormalizeDriver(in); err == nil {
			t.Fatalf("NormalizeDriver(%q): expected error", in)
		}
	}
}

func TestOpenDSN(t *testing.T) {
	t.Parallel()

	_, err := openDSN("  ")
	require.ErrorContains(t, err, "database DSN is empty")

	db, err := openDSN("postgres://blog@127.0.0.1:1/blog")
	require.NoError(t, err, "opening is lazy; nothing is dialled yet")
	require.NoError(t, db.Close())
}
