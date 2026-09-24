// Package ports declares the permission enforcer used by authorization middleware.
package ports

import (
	"context"

	"github.com/google/uuid"
)

// Enforcer decides whether a user holds a permission.
type Enforcer interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
}
