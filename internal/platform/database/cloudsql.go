// Package database opens GORM and database/sql pools for PostgreSQL, directly or via Cloud SQL.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/cloudsqlconn"
	"cloud.google.com/go/cloudsqlconn/postgres/pgxv5"
	"github.com/turahe/blog-api/internal/platform/config"
)

const cloudSQLPostgresDriver = "cloudsql-postgres"

func openCloudSQL(_ context.Context, cfg config.Config) (*sql.DB, func() error, error) {
	if cfg.DBInstanceConnectionName == "" {
		return nil, nil, errors.New("DB_INSTANCE_CONNECTION_NAME is required for Cloud SQL")
	}

	if cfg.DBName == "" {
		return nil, nil, errors.New("DB_NAME is required for Cloud SQL")
	}

	if cfg.DBUser == "" {
		return nil, nil, errors.New("DB_USER is required for Cloud SQL")
	}

	opts, err := dialerOptions(cfg)
	if err != nil {
		return nil, nil, err
	}

	return openCloudSQLPostgres(cfg, opts)
}

func dialerOptions(cfg config.Config) ([]cloudsqlconn.Option, error) {
	opts := make([]cloudsqlconn.Option, 0, 4)
	if cfg.DBPrivateIPEnabled {
		opts = append(opts, cloudsqlconn.WithDefaultDialOptions(cloudsqlconn.WithPrivateIP()))
	}

	if cfg.DBIAMAuthEnabled {
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
			return nil, errors.New("DB_GOOGLE_CREDENTIALS_SOURCE path is empty")
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
