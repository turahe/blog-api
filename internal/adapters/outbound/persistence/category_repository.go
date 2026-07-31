package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryports "github.com/turahe/blog-api/internal/core/category/ports"
	"gorm.io/gorm"
)

var _ categoryports.Repository = (*CategoryRepository)(nil)

type CategoryModel struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Name        string
	Slug        string
	Description *string
	ParentID    *uuid.UUID `gorm:"type:uuid;column:parent_id"`
	ImageID     *uuid.UUID `gorm:"type:uuid;column:image_id"`
	Lft         int        `gorm:"column:lft"`
	Rgt         int        `gorm:"column:rgt"`
	Depth       int        `gorm:"column:depth"`
	SortOrder   int        `gorm:"column:sort_order"`
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
	if err := r.db.WithContext(ctx).Order("lft ASC").Find(&models).Error; err != nil {
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
	model := categoryToModel(cat)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return categorydomain.Category{}, err
	}
	return mapCategory(model), nil
}

func (r *CategoryRepository) Update(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	res := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("id = ?", cat.ID).
		Updates(map[string]any{
			"name":        cat.Name,
			"slug":        cat.Slug,
			"description": nullableString(cat.Description),
			"parent_id":   cat.ParentID,
			"image_id":    cat.ImageID,
			"updated_at":  cat.UpdatedAt,
		})
	if res.Error != nil {
		return categorydomain.Category{}, res.Error
	}
	if res.RowsAffected == 0 {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return r.GetByID(ctx, cat.ID)
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
	var n int64
	err := r.db.WithContext(ctx).Model(&CategoryModel{}).
		Where("slug = ? AND id <> ?", slug, excludeID).
		Count(&n).Error
	return n > 0, err
}

func (r *CategoryRepository) CountPosts(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Table("posts").Where("category_id = ?", categoryID).Count(&n).Error
	return n, err
}

func (r *CategoryRepository) CountChildren(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("parent_id = ?", categoryID).Count(&n).Error
	return n, err
}

func (r *CategoryRepository) ReplaceTreeBounds(ctx context.Context, cats []categorydomain.Category) error {
	for _, cat := range cats {
		if err := r.db.WithContext(ctx).Model(&CategoryModel{}).
			Where("id = ?", cat.ID).
			Updates(map[string]any{
				"lft":        cat.Lft,
				"rgt":        cat.Rgt,
				"depth":      cat.Depth,
				"sort_order": cat.SortOrder,
				"parent_id":  cat.ParentID,
				"updated_at": cat.UpdatedAt,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *CategoryRepository) WithinTx(ctx context.Context, fn func(ctx context.Context, repo categoryports.Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &CategoryRepository{db: tx})
	})
}

func mapCategory(model CategoryModel) categorydomain.Category {
	cat := categorydomain.Category{
		ID:        model.ID,
		Name:      model.Name,
		Slug:      model.Slug,
		ParentID:  model.ParentID,
		ImageID:   model.ImageID,
		Lft:       model.Lft,
		Rgt:       model.Rgt,
		Depth:     model.Depth,
		SortOrder: model.SortOrder,
		CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
	}
	if model.Description != nil {
		cat.Description = *model.Description
	}
	return cat
}

func categoryToModel(cat categorydomain.Category) CategoryModel {
	return CategoryModel{
		ID:          cat.ID,
		Name:        cat.Name,
		Slug:        cat.Slug,
		Description: nullableString(cat.Description),
		ParentID:    cat.ParentID,
		ImageID:     cat.ImageID,
		Lft:         cat.Lft,
		Rgt:         cat.Rgt,
		Depth:       cat.Depth,
		SortOrder:   cat.SortOrder,
		CreatedAt:   cat.CreatedAt,
		UpdatedAt:   cat.UpdatedAt,
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
