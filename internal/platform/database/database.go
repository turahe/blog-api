package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	// database/sql driver registered for the raw *sql.DB pool (migrations, health).
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/turahe/blog-api/internal/platform/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DriverPostgres is the only driver accepted by DB_DRIVER.
const DriverPostgres = config.DBDriverPostgres

// Database wraps a GORM handle and the underlying sql.DB pool.
type Database struct {
	Driver  string
	GORM    *gorm.DB
	SQL     *sql.DB
	cleanup func() error
}

// Open connects using split DB_* settings (direct) or Cloud SQL when
// DB_INSTANCE_CONNECTION_NAME is set.
func Open(ctx context.Context, cfg config.Config) (*Database, error) {
	driver, err := NormalizeDriver(cfg.DBDriver)
	if err != nil {
		return nil, err
	}

	var (
		sqlDB   *sql.DB
		cleanup func() error
	)
	if cfg.UsesCloudSQL() {
		sqlDB, cleanup, err = openCloudSQL(ctx, cfg)
	} else {
		dsn, dsnErr := cfg.DatabaseDSN()
		if dsnErr != nil {
			return nil, dsnErr
		}

		sqlDB, err = openDSN(dsn)
	}

	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.DBMaxOpen)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdle)
	sqlDB.SetConnMaxLifetime(cfg.DBMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.DBMaxIdleTime)

	closeAll := func() {
		_ = sqlDB.Close()

		if cleanup != nil {
			_ = cleanup()
		}
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		closeAll()

		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		closeAll()

		return nil, fmt.Errorf("open gorm (%s): %w", driver, err)
	}

	return &Database{Driver: driver, GORM: gdb, SQL: sqlDB, cleanup: cleanup}, nil
}

// Close releases the sql pool and Cloud SQL dialer (if any).
func (db *Database) Close() error {
	if db == nil {
		return nil
	}

	var errs []error

	if db.SQL != nil {
		if err := db.SQL.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close sql pool: %w", err))
		}
	}

	if db.cleanup != nil {
		if err := db.cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("close cloud sql dialer: %w", err))
		}
	}

	return errors.Join(errs...)
}

// NormalizeDriver maps aliases to the canonical driver name.
func NormalizeDriver(raw string) (string, error) {
	return config.NormalizeDBDriver(raw)
}

func openDSN(dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("database DSN is empty when DB_INSTANCE_CONNECTION_NAME is empty")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	return db, nil
}
