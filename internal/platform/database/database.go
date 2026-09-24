package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	// database/sql drivers registered for the raw *sql.DB pool (migrations, health).
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/turahe/blog-api/internal/platform/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Driver names accepted by DB_DRIVER.
const (
	DriverPostgres  = config.DBDriverPostgres
	DriverMySQL     = config.DBDriverMySQL
	DriverSQLServer = config.DBDriverSQLServer
)

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
		sqlDB, cleanup, err = openCloudSQL(ctx, driver, cfg)
	} else {
		dsn, dsnErr := cfg.DatabaseDSN()
		if dsnErr != nil {
			return nil, dsnErr
		}

		sqlDB, err = openDSN(driver, dsn)
	}

	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.DBMaxOpen)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdle)
	sqlDB.SetConnMaxLifetime(cfg.DBMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.DBMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()

		if cleanup != nil {
			_ = cleanup()
		}

		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}

	dialector, err := gormDialector(driver, sqlDB)
	if err != nil {
		_ = sqlDB.Close()

		if cleanup != nil {
			_ = cleanup()
		}

		return nil, err
	}

	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		_ = sqlDB.Close()

		if cleanup != nil {
			_ = cleanup()
		}

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

// NormalizeDriver maps aliases to canonical driver names.
func NormalizeDriver(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DriverPostgres, "postgresql", "pg":
		return DriverPostgres, nil
	case DriverMySQL, "mariadb":
		return DriverMySQL, nil
	case DriverSQLServer, "mssql":
		return DriverSQLServer, nil
	default:
		return "", fmt.Errorf("unsupported DB_DRIVER %q (want postgres, mysql, or sqlserver)", raw)
	}
}

func openDSN(driver, dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("database DSN is empty when DB_INSTANCE_CONNECTION_NAME is empty")
	}

	var (
		sqlDriver string
		openDSN   string
	)

	switch driver {
	case DriverPostgres:
		sqlDriver = "pgx"
		openDSN = dsn
	case DriverMySQL:
		sqlDriver = "mysql"
		openDSN = normalizeMySQLDSN(dsn)
	case DriverSQLServer:
		sqlDriver = "sqlserver"
		openDSN = dsn
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}

	db, err := sql.Open(sqlDriver, openDSN)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}

	return db, nil
}

func normalizeMySQLDSN(dsn string) string {
	if after, ok := strings.CutPrefix(dsn, "mysql://"); ok {
		return after
	}

	return dsn
}

func gormDialector(driver string, sqlDB *sql.DB) (gorm.Dialector, error) {
	switch driver {
	case DriverPostgres:
		return postgres.New(postgres.Config{Conn: sqlDB}), nil
	case DriverMySQL:
		return mysql.New(mysql.Config{Conn: sqlDB}), nil
	case DriverSQLServer:
		return sqlserver.New(sqlserver.Config{Conn: sqlDB}), nil
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
}
