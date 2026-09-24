package persistence

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

// IsUniqueViolation reports whether err is a PostgreSQL unique-constraint violation.
func IsUniqueViolation(err error) bool {
	return isUniqueViolation(err)
}

func isUniqueViolation(err error) bool {
	return uniqueConstraint(err) != ""
}

// uniqueConstraint returns the violated unique constraint or index name, or "".
func uniqueConstraint(err error) string {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != pgUniqueViolation {
		return ""
	}

	if pgErr.ConstraintName == "" {
		return "unknown"
	}

	return pgErr.ConstraintName
}
