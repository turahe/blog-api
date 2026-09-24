// Package ports declares the user repository and service interfaces.
package ports

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Repository reads user accounts.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}

// Service is the user use-case API consumed by HTTP handlers.
type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}
