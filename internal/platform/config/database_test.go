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

func TestDatabaseDSNMySQL(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DBDriver:   "mysql",
		DBHost:     "db.example.com",
		DBPort:     3306,
		DBUser:     "blog",
		DBPassword: "secret",
		DBName:     "blog",
	}
	got, err := cfg.DatabaseDSN()
	if err != nil {
		t.Fatal(err)
	}
	want := "blog:secret@tcp(db.example.com:3306)/blog?parseTime=true"
	if got != want {
		t.Fatalf("DatabaseDSN()=%q want %q", got, want)
	}
}

func TestDatabaseDSNSQLServer(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DBDriver:   "sqlserver",
		DBHost:     "db.example.com",
		DBPort:     1433,
		DBUser:     "blog",
		DBPassword: "secret",
		DBName:     "blog",
	}
	got, err := cfg.DatabaseDSN()
	if err != nil {
		t.Fatal(err)
	}
	want := "sqlserver://blog:secret@db.example.com:1433?database=blog"
	if got != want {
		t.Fatalf("DatabaseDSN()=%q want %q", got, want)
	}
}

func TestLoadDatabaseFromSplitEnvironment(t *testing.T) {
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
