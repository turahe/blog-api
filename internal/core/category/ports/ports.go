package ports

import (
	"context"

	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

type Repository interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
}

type Service interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
}
