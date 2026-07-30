package ports

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}

type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}
