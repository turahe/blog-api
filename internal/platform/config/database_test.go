package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDatabaseDSNPostgres(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DBDriver:   "postgres",
		DBHost:     "db.example.com",
		DBPort:     5432,
		DBUser:     "blog",
		DBPassword: "p@ss word",
		DBName:     "blog",
		DBSSLMode:  "require",
	}

	got, err := cfg.DatabaseDSN()
	if err != nil {
		t.Fatal(err)
	}

	want := "postgres://blog:p%40ss%20word@db.example.com:5432/blog?sslmode=require"
	if got != want {
		t.Fatalf("DatabaseDSN()=%q want %q", got, want)
	}
}

func TestDatabaseDSNDefaultsPortAndSSLMode(t *testing.T) {
	t.Parallel()

	got, err := Config{DBHost: "db", DBUser: "blog", DBPassword: "pw", DBName: "blog"}.DatabaseDSN()
	require.NoError(t, err)
	require.Equal(t, "postgres://blog:pw@db:5432/blog?sslmode=disable", got)
}

func TestDatabaseRejectsNonPostgresDrivers(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"mysql", "sqlserver"} {
		cfg := Config{DBDriver: driver, DBHost: "db", DBUser: "blog", DBName: "blog"}
		if _, err := cfg.DatabaseDSN(); err == nil {
			t.Fatalf("DatabaseDSN(%s): expected error", driver)
		}

		if err := cfg.ValidateDatabase(); err == nil {
			t.Fatalf("ValidateDatabase(%s): expected error", driver)
		}
	}
}

func TestLoadDatabaseFromSplitEnvironment(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "blog")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "blog")
	t.Setenv("DB_SSLMODE", "require")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DBHost != "db.internal" || cfg.DBPort != 5433 || cfg.DBSSLMode != "require" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestLoadRejectsProductionPostgresWithoutTLS(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_KEY", "production-session-key-32bytes-min!")
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_USER", "blog")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "blog")
	t.Setenv("DB_SSLMODE", "disable")

	if _, err := Load(); err == nil {
		t.Fatal("expected production TLS error")
	}
}

func TestValidateDatabaseRequiresHostForDirect(t *testing.T) {
	t.Parallel()

	cfg := Config{DBDriver: "postgres", DBUser: "blog", DBName: "blog", DBPort: 5432, DBSSLMode: "disable"}
	if err := cfg.ValidateDatabase(); err == nil {
		t.Fatal("expected host error")
	}
}

func TestValidateDatabaseSkippedForCloudSQL(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DBInstanceConnectionName: "proj:region:inst",
		DBUser:                   "blog",
		DBName:                   "blog",
	}
	if err := cfg.ValidateDatabase(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDatabase(t *testing.T) {
	t.Parallel()

	valid := Config{DBDriver: "pg", DBHost: "db", DBUser: "blog", DBName: "blog"}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "defaults port and ssl mode outside production", mutate: func(*Config) {}},
		{name: "port too large", mutate: func(c *Config) { c.DBPort = 70000 }, wantErr: "DB_PORT must be between 1 and 65535 (got 70000)"},
		{name: "negative port", mutate: func(c *Config) { c.DBPort = -1 }, wantErr: "DB_PORT"},
		{name: "blank user", mutate: func(c *Config) { c.DBUser = " " }, wantErr: "DB_USER"},
		{name: "blank name", mutate: func(c *Config) { c.DBName = "" }, wantErr: "DB_NAME"},
		{name: "default ssl mode in production", mutate: func(c *Config) { c.Environment = envProduction }, wantErr: "DB_SSLMODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)

			err := cfg.ValidateDatabase()
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
