package connect

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
)

func TestOpenDatabaseRejectsInvalidSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{name: "unsupported driver", cfg: config.Config{DBDriver: "mysql"}, wantErr: "unsupported DB_DRIVER"},
		{
			name:    "unsupported sslmode",
			cfg:     config.Config{DBSSLMode: "verify-full"},
			wantErr: "DB_SSLMODE",
		},
		{
			name: "missing cloud sql database name",
			cfg: config.Config{
				DBInstanceConnectionName: "project:region:instance", DBUser: "blog",
			},
			wantErr: "DB_NAME is required",
		},
		{
			name: "missing cloud sql user",
			cfg: config.Config{
				DBInstanceConnectionName: "project:region:instance", DBName: "blog",
			},
			wantErr: "DB_USER is required",
		},
		{
			name: "unsupported credentials source",
			cfg: config.Config{
				DBInstanceConnectionName: "project:region:instance", DBName: "blog", DBUser: "blog",
				DBGoogleCredentialsSource: "vault",
			},
			wantErr: "unsupported",
		},
		{
			name: "empty credentials path",
			cfg: config.Config{
				DBInstanceConnectionName: "project:region:instance", DBName: "blog", DBUser: "blog",
				DBGoogleCredentialsSource: "path:",
			},
			wantErr: "path is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := OpenDatabase(t.Context(), tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, db)
		})
	}
}
