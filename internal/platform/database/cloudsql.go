package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	"cloud.google.com/go/cloudsqlconn"
	cloudmysql "cloud.google.com/go/cloudsqlconn/mysql/mysql"
	"cloud.google.com/go/cloudsqlconn/postgres/pgxv5"
	cloudmssql "cloud.google.com/go/cloudsqlconn/sqlserver/mssql"
	"github.com/turahe/blog-api/internal/platform/config"
)

const (
	cloudSQLPostgresDriver  = "cloudsql-postgres"
	cloudSQLMySQLDriver     = "cloudsql-mysql"
	cloudSQLSQLServerDriver = "cloudsql-sqlserver"
)

func openCloudSQL(_ context.Context, driver string, cfg config.Config) (*sql.DB, func() error, error) {
	if cfg.DBInstanceConnectionName == "" {
		return nil, nil, fmt.Errorf("DB_INSTANCE_CONNECTION_NAME is required for Cloud SQL")
	}
	if cfg.DBName == "" {
		return nil, nil, fmt.Errorf("DB_NAME is required for Cloud SQL")
	}
	if cfg.DBUser == "" {
		return nil, nil, fmt.Errorf("DB_USER is required for Cloud SQL")
	}

	opts, err := dialerOptions(cfg)
	if err != nil {
		return nil, nil, err
	}

	switch driver {
	case DriverPostgres:
		return openCloudSQLPostgres(cfg, opts)
	case DriverMySQL:
		return openCloudSQLMySQL(cfg, opts)
	case DriverSQLServer:
		return openCloudSQLSQLServer(cfg, opts)
	default:
		return nil, nil, fmt.Errorf("unsupported Cloud SQL driver %q", driver)
	}
}

func dialerOptions(cfg config.Config) ([]cloudsqlconn.Option, error) {
	opts := make([]cloudsqlconn.Option, 0, 4)
	if cfg.DBPrivateIPEnabled {
		opts = append(opts, cloudsqlconn.WithDefaultDialOptions(cloudsqlconn.WithPrivateIP()))
	}
	normalized, err := NormalizeDriver(cfg.DBDriver)
	if err != nil {
		return nil, err
	}
	if cfg.DBIAMAuthEnabled && normalized != DriverSQLServer {
		opts = append(opts, cloudsqlconn.WithIAMAuthN())
	}
	credOpts, err := credentialsOptions(cfg.DBGoogleCredentialsSource)
	if err != nil {
		return nil, err
	}
	opts = append(opts, credOpts...)
	return opts, nil
}

func credentialsOptions(source string) ([]cloudsqlconn.Option, error) {
	source = strings.TrimSpace(source)
	switch {
	case source == "" || source == "workload-identity" || source == "adc":
		return nil, nil
	case strings.HasPrefix(source, "path:"):
		path := strings.TrimSpace(strings.TrimPrefix(source, "path:"))
		if path == "" {
			return nil, fmt.Errorf("DB_GOOGLE_CREDENTIALS_SOURCE path is empty")
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("DB_GOOGLE_CREDENTIALS_SOURCE credentials file: %w", err)
		}
		return []cloudsqlconn.Option{cloudsqlconn.WithCredentialsFile(path)}, nil
	default:
		return nil, fmt.Errorf("unsupported DB_GOOGLE_CREDENTIALS_SOURCE %q", source)
	}
}

func openCloudSQLPostgres(cfg config.Config, opts []cloudsqlconn.Option) (*sql.DB, func() error, error) {
	cleanup, err := pgxv5.RegisterDriver(cloudSQLPostgresDriver, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("register cloudsql postgres driver: %w", err)
	}
	dsn := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable",
		cfg.DBInstanceConnectionName, cfg.DBUser, cfg.DBName)
	if !cfg.DBIAMAuthEnabled && cfg.DBPassword != "" {
		dsn += " password=" + cfg.DBPassword
	}
	db, err := sql.Open(cloudSQLPostgresDriver, dsn)
	if err != nil {
		_ = cleanup()
		return nil, nil, fmt.Errorf("open cloudsql postgres: %w", err)
	}
	return db, cleanup, nil
}

func openCloudSQLMySQL(cfg config.Config, opts []cloudsqlconn.Option) (*sql.DB, func() error, error) {
	cleanup, err := cloudmysql.RegisterDriver(cloudSQLMySQLDriver, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("register cloudsql mysql driver: %w", err)
	}
	userInfo := url.User(cfg.DBUser)
	if !cfg.DBIAMAuthEnabled {
		userInfo = url.UserPassword(cfg.DBUser, cfg.DBPassword)
	}
	dsn := fmt.Sprintf("%s@%s(%s)/%s?parseTime=true&allowCleartextPasswords=true",
		userInfo.String(), cloudSQLMySQLDriver, cfg.DBInstanceConnectionName, cfg.DBName)
	db, err := sql.Open(cloudSQLMySQLDriver, dsn)
	if err != nil {
		_ = cleanup()
		return nil, nil, fmt.Errorf("open cloudsql mysql: %w", err)
	}
	return db, cleanup, nil
}

func openCloudSQLSQLServer(cfg config.Config, opts []cloudsqlconn.Option) (*sql.DB, func() error, error) {
	if cfg.DBPassword == "" {
		return nil, nil, fmt.Errorf("DB_PASSWORD is required for Cloud SQL SQL Server")
	}
	cleanup, err := cloudmssql.RegisterDriver(cloudSQLSQLServerDriver, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("register cloudsql sqlserver driver: %w", err)
	}
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(cfg.DBUser, cfg.DBPassword),
		Host:   "localhost",
	}
	q := u.Query()
	q.Set("database", cfg.DBName)
	q.Set("cloudsql", cfg.DBInstanceConnectionName)
	u.RawQuery = q.Encode()
	db, err := sql.Open(cloudSQLSQLServerDriver, u.String())
	if err != nil {
		_ = cleanup()
		return nil, nil, fmt.Errorf("open cloudsql sqlserver: %w", err)
	}
	return db, cleanup, nil
}
