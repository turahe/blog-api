package ports

import (
	"context"

	"github.com/google/uuid"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

type Repository interface {
	List(ctx context.Context) ([]tagdomain.Tag, error)
	GetByID(ctx context.Context, id uuid.UUID) (tagdomain.Tag, error)
	GetBySlug(ctx context.Context, slug string) (tagdomain.Tag, error)
	Create(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error)
	Update(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error)
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	CountPosts(ctx context.Context, tagID uuid.UUID) (int64, error)
	MergeInto(ctx context.Context, sourceID, targetID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
	ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error)
}

type Service interface {
	List(ctx context.Context) ([]tagdomain.Tag, error)
	Create(ctx context.Context, name, slug string) (tagdomain.Tag, error)
	Update(ctx context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error)
	Merge(ctx context.Context, sourceID, intoID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	ResolveOrCreate(ctx context.Context, names []string) ([]tagdomain.Tag, error)
	ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
	ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error)
}
