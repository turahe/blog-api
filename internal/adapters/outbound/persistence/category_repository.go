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
	Name        string    `gorm:"not null"`
	Slug        string    `gorm:"not null;uniqueIndex"`
	Description *string
	ParentID    *uuid.UUID `gorm:"type:uuid;column:parent_id"`
	ImageID     *uuid.UUID `gorm:"type:uuid;column:image_id"`
	SortOrder   int        `gorm:"column:sort_order;not null;default:0"`
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
	if err := r.db.WithContext(ctx).Order("sort_order ASC, name ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]categorydomain.Category, 0, len(models))
	for _, model := range models {
		out = append(out, mapCategory(model))
	}
	return out, nil
}

func (r *CategoryRepository) GetByID(ctx context.Context, id uuid.UUID) (categorydomain.Category, error) {
	var model CategoryModel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	if err != nil {
		return categorydomain.Category{}, err
	}
	return mapCategory(model), nil
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

func (r *CategoryRepository) Create(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	model := toCategoryModel(cat)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return categorydomain.Category{}, err
	}
	return mapCategory(model), nil
}

func (r *CategoryRepository) Update(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	model := toCategoryModel(cat)
	res := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("id = ?", cat.ID).Updates(map[string]any{
		"name":        model.Name,
		"slug":        model.Slug,
		"description": model.Description,
		"parent_id":   model.ParentID,
		"image_id":    model.ImageID,
		"sort_order":  model.SortOrder,
		"updated_at":  model.UpdatedAt,
	})
	if res.Error != nil {
		return categorydomain.Category{}, res.Error
	}
	if res.RowsAffected == 0 {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return cat, nil
}

func (r *CategoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&CategoryModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return categorydomain.ErrNotFound
	}
	return nil
}

func (r *CategoryRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	q := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("slug = ?", slug)
	if excludeID != uuid.Nil {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *CategoryRepository) CountChildren(ctx context.Context, parentID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("parent_id = ?", parentID).Count(&count).Error
	return count, err
}

func (r *CategoryRepository) ListSiblingIDs(ctx context.Context, parentID *uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	q := r.db.WithContext(ctx).Model(&CategoryModel{}).Order("sort_order ASC, name ASC")
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	if err := q.Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *CategoryRepository) MaxSortOrder(ctx context.Context, parentID *uuid.UUID) (int, error) {
	var max *int
	q := r.db.WithContext(ctx).Model(&CategoryModel{}).Select("MAX(sort_order)")
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	if err := q.Scan(&max).Error; err != nil {
		return -1, err
	}
	if max == nil {
		return -1, nil
	}
	return *max, nil
}

func (r *CategoryRepository) Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range orderedIDs {
			q := tx.Model(&CategoryModel{}).Where("id = ?", id)
			if parentID == nil {
				q = q.Where("parent_id IS NULL")
			} else {
				q = q.Where("parent_id = ?", *parentID)
			}
			res := q.Update("sort_order", i)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return categorydomain.ErrNotFound
			}
		}
		return nil
	})
}

func toCategoryModel(cat categorydomain.Category) CategoryModel {
	model := CategoryModel{
		ID:        cat.ID,
		Name:      cat.Name,
		Slug:      cat.Slug,
		ParentID:  cat.ParentID,
		ImageID:   cat.ImageID,
		SortOrder: cat.SortOrder,
		CreatedAt: cat.CreatedAt,
		UpdatedAt: cat.UpdatedAt,
	}
	if cat.Description != "" {
		desc := cat.Description
		model.Description = &desc
	}
	return model
}

func mapCategory(model CategoryModel) categorydomain.Category {
	cat := categorydomain.Category{
		ID:        model.ID,
		Name:      model.Name,
		Slug:      model.Slug,
		ParentID:  model.ParentID,
		ImageID:   model.ImageID,
		SortOrder: model.SortOrder,
		CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
	}
	if model.Description != nil {
		cat.Description = *model.Description
	}
	return cat
}
