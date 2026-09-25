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
	// GetByID returns a live (not soft-deleted) post.
	GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	// GetDeletedByID returns a soft-deleted post.
	GetDeletedByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	// Create inserts post; a slug held by a live post returns postdomain.ErrConflict.
	Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	// Update persists a live post only while the stored version is post.Version-1;
	// otherwise it returns postdomain.ErrStaleVersion.
	Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	// SoftDelete sets deleted_at on a live post at version post.Version-1.
	SoftDelete(ctx context.Context, post postdomain.Post) error
	// Restore clears deleted_at and persists post's slug, status, and version on a
	// soft-deleted post at version post.Version-1.
	Restore(ctx context.Context, post postdomain.Post) (postdomain.Post, error)
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	// SlugsWithPrefix returns the live slugs equal to base or starting with base + "-".
	SlugsWithPrefix(ctx context.Context, base string) ([]string, error)
	SetCoverImage(ctx context.Context, postID uuid.UUID, mediaID *uuid.UUID, updatedAt time.Time) error
}

// PublishNotifier tells users that a post went public. Delivery is best effort:
// implementations log failures instead of returning them.
type PublishNotifier interface {
	// PostPublished runs after post became published; actorID is who published it, or nil
	// when no user did.
	PostPublished(ctx context.Context, post postdomain.Post, actorID *uuid.UUID)
}

// Service is the post use-case API consumed by HTTP handlers.
type Service interface {
	ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error)
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error)
	CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error)
	Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	PublishBy(ctx context.Context, actorID, id uuid.UUID) (postdomain.Post, error)
	Unpublish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Archive(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error)
	ReplaceMedia(ctx context.Context, postID uuid.UUID, items []mediadomain.PostMediaItem, enforceCoverConsistency bool) ([]mediadomain.PostMediaItem, error)
}
