package config

import "testing"

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
