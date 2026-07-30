package ports

import (
	"context"

	"github.com/google/uuid"
)

type Enforcer interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
}
