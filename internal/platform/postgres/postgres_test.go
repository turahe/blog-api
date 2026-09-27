package postgres

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestOpen(t *testing.T) {
	t.Parallel()

	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping postgres test against PostgreSQL")
	}

	u, err := url.Parse(raw)
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	password, _ := u.User.Password()

	db, err := Open(t.Context(), config.Config{
		DBHost: u.Hostname(), DBPort: port, DBUser: u.User.Username(), DBPassword: password,
		DBName: strings.TrimPrefix(u.Path, "/"), DBSSLMode: u.Query().Get("sslmode"), DBMaxOpen: 1,
	})
	require.NoError(t, err)
	require.NoError(t, db.SQL.PingContext(t.Context()))
	require.NoError(t, db.Close())
}

func TestOpenRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	db, err := Open(t.Context(), config.Config{DBDriver: "mysql"})
	require.ErrorContains(t, err, "unsupported DB_DRIVER")
	require.Nil(t, db)
}
