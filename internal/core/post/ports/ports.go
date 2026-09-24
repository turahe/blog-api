// Package ports declares the post repository, tag linker, and service interfaces.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// TagLinker resolves tag names and links tags to posts.
type TagLinker interface {
	ResolveOrCreate(ctx context.Context, names []string) ([]tagdomain.Tag, error)
	ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error
	ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error)
}

// Repository stores posts.
type Repository interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	// Update persists post only while the stored version is post.Version-1;
	// otherwise it returns postdomain.ErrStaleVersion.
	Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	SetCoverImage(ctx context.Context, postID uuid.UUID, mediaID *uuid.UUID, updatedAt time.Time) error
}

// Service is the post use-case API consumed by HTTP handlers.
type Service interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error)
	Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error)
	ReplaceMedia(ctx context.Context, postID uuid.UUID, items []mediadomain.PostMediaItem, enforceCoverConsistency bool) ([]mediadomain.PostMediaItem, error)
}
