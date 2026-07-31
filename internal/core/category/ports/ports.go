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
	CountChildren(ctx context.Context, parentID uuid.UUID) (int64, error)
	ListSiblingIDs(ctx context.Context, parentID *uuid.UUID) ([]uuid.UUID, error)
	MaxSortOrder(ctx context.Context, parentID *uuid.UUID) (int, error)
	Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error
}

type Service interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
	Create(ctx context.Context, in categorydomain.CreateInput) (categorydomain.Category, error)
	Update(ctx context.Context, id uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error
}
