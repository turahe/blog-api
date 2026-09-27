package database

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

func writeFile(t *testing.T, contents []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))

	return path
}

// serviceAccountFile writes syntactically valid service-account credentials. Nothing in
// these tests exchanges them for a token.
func serviceAccountFile(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	contents, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "test", "private_key_id": "1",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "test@test.iam.gserviceaccount.com", "client_id": "1",
		"token_uri": "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)

	return writeFile(t, contents)
}

func TestOpenCloudSQLErrors(t *testing.T) {
	t.Parallel()

	valid := config.Config{DBInstanceConnectionName: "project:region:instance", DBName: "blog", DBUser: "blog"}

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{name: "missing instance", mutate: func(c *config.Config) { c.DBInstanceConnectionName = "" }, wantErr: "DB_INSTANCE_CONNECTION_NAME is required"},
		{name: "missing database name", mutate: func(c *config.Config) { c.DBName = "" }, wantErr: "DB_NAME is required"},
		{name: "missing user", mutate: func(c *config.Config) { c.DBUser = "" }, wantErr: "DB_USER is required"},
		{name: "unsupported credentials source", mutate: func(c *config.Config) { c.DBGoogleCredentialsSource = "vault" }, wantErr: "unsupported"},
		{
			name:    "malformed credentials file",
			mutate:  func(c *config.Config) { c.DBGoogleCredentialsSource = "path:" + writeFile(t, []byte("{")) },
			wantErr: "register cloudsql postgres driver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)

			db, cleanup, err := openCloudSQL(t.Context(), cfg)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, db)
			require.Nil(t, cleanup)
		})
	}
}

func TestDialerOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		want    int
		wantErr string
	}{
		{name: "defaults", want: 0},
		{name: "private ip", cfg: config.Config{DBPrivateIPEnabled: true}, want: 1},
		{name: "iam auth", cfg: config.Config{DBIAMAuthEnabled: true}, want: 1},
		{
			name: "private ip, iam auth and credentials file",
			cfg:  config.Config{DBPrivateIPEnabled: true, DBIAMAuthEnabled: true, DBGoogleCredentialsSource: "path:" + writeFile(t, []byte("{}"))},
			want: 3,
		},
		{name: "bad credentials source", cfg: config.Config{DBGoogleCredentialsSource: "vault"}, wantErr: "unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := dialerOptions(tt.cfg)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, opts, tt.want)
		})
	}
}

func TestCredentialsOptions(t *testing.T) {
	t.Parallel()

	file := writeFile(t, []byte("{}"))

	tests := []struct {
		name    string
		source  string
		want    int
		wantErr string
		wantIs  error
	}{
		{name: "unset uses ADC", source: ""},
		{name: "adc", source: " adc "},
		{name: "workload identity", source: "workload-identity"},
		{name: "credentials file", source: "path: " + file, want: 1},
		{name: "empty path", source: "path:  ", wantErr: "path is empty"},
		{name: "missing file", source: "path:" + filepath.Join(t.TempDir(), "missing.json"), wantErr: "credentials file", wantIs: fs.ErrNotExist},
		{name: "unsupported", source: "vault", wantErr: `unsupported DB_GOOGLE_CREDENTIALS_SOURCE "vault"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := credentialsOptions(tt.source)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				if tt.wantIs != nil {
					require.ErrorIs(t, err, tt.wantIs)
				}

				return
			}

			require.NoError(t, err)
			require.Len(t, opts, tt.want)
		})
	}
}

// cloudSQLDriverRegistered guards the one successful registration a test process can make:
// the driver is registered under a fixed name, and database/sql panics on a second one.
var cloudSQLDriverRegistered atomic.Bool

func TestOpenCloudSQLPingFailure(t *testing.T) {
	t.Parallel()

	if cloudSQLDriverRegistered.Swap(true) {
		t.Skip("the Cloud SQL driver can only be registered once per process")
	}

	// A malformed instance name fails inside the dialer, before any network call.
	_, err := Open(t.Context(), config.Config{
		DBInstanceConnectionName: "not-an-instance", DBName: "blog", DBUser: "blog", DBPassword: "secret",
		DBGoogleCredentialsSource: "path:" + serviceAccountFile(t), DBMaxOpen: 1,
	})
	require.ErrorContains(t, err, "ping postgres")
}
