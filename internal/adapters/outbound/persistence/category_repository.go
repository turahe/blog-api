package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"gorm.io/gorm"
)

type CategoryModel struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name        string
	Slug        string
	Description *string
	ParentID    *uuid.UUID `gorm:"type:uuid;column:parent_id"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (CategoryModel) TableName() string { return "categories" }

type CategoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

func (r *CategoryRepository) List(ctx context.Context) ([]categorydomain.Category, error) {
	var models []CategoryModel
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]categorydomain.Category, 0, len(models))
	for _, model := range models {
		out = append(out, mapCategory(model))
	}
	return out, nil
}

func (r *CategoryRepository) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	var model CategoryModel
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	if err != nil {
		return categorydomain.Category{}, err
	}
	return mapCategory(model), nil
}

func mapCategory(model CategoryModel) categorydomain.Category {
	cat := categorydomain.Category{
		ID: model.ID, Name: model.Name, Slug: model.Slug,
		ParentID: model.ParentID, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
	if model.Description != nil {
		cat.Description = *model.Description
	}
	return cat
}
