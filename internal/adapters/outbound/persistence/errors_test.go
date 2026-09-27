package persistence

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		wantConstraint string
	}{
		{name: "nil"},
		{name: "plain error", err: errors.New("boom")},
		{name: "other postgres error", err: &pgconn.PgError{Code: "23503", ConstraintName: "posts_author_fk"}},
		{name: "named constraint", err: &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: "tags_slug_key"}, wantConstraint: "tags_slug_key"},
		{name: "unnamed constraint", err: &pgconn.PgError{Code: pgUniqueViolation}, wantConstraint: "unknown"},
		{
			name:           "wrapped",
			err:            fmt.Errorf("create: %w", &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: "users_email_unique"}),
			wantConstraint: "users_email_unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.wantConstraint, uniqueConstraint(tt.err))
			require.Equal(t, tt.wantConstraint != "", IsUniqueViolation(tt.err))
		})
	}
}
