package ports

import (
	"context"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

type Repository interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetByID(ctx context.Context, id uuid.UUID) (categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
	Create(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error)
	Update(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)
	CountPosts(ctx context.Context, categoryID uuid.UUID) (int64, error)
	CountChildren(ctx context.Context, categoryID uuid.UUID) (int64, error)
	ReplaceTreeBounds(ctx context.Context, cats []categorydomain.Category) error
	WithinTx(ctx context.Context, fn func(ctx context.Context, r Repository) error) error
}

type CreateInput struct {
	Name        string
	Slug        string
	Description *string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
	BeforeID    *uuid.UUID
}

type UpdateInput struct {
	Name            *string
	Slug            *string
	Description     *string
	ImageID         *uuid.UUID
	ImageIDProvided bool
}

type Service interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
	Create(ctx context.Context, in CreateInput) (categorydomain.Category, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Move(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, beforeID *uuid.UUID) (categorydomain.Category, error)
	RebuildAll(ctx context.Context) error
}
