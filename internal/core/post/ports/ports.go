package ports

import (
	"context"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type Repository interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
}

type Service interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID) (postdomain.Post, error)
	Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
}
