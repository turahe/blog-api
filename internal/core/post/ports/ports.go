package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type Repository interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	SetCoverImage(ctx context.Context, postID uuid.UUID, mediaID *uuid.UUID, updatedAt time.Time) error
}

type Service interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID) (postdomain.Post, error)
	Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error)
	ReplaceMedia(ctx context.Context, postID uuid.UUID, items []mediadomain.PostMediaItem, enforceCoverConsistency bool) ([]mediadomain.PostMediaItem, error)
}

