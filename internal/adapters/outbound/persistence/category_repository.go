// Package persistence implements the core repository ports with GORM (bigint ids internally, UUIDs at the boundary).
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

var categoryColumns = withRefs("categories",
	uuidRef("categories", "categories.parent_id", "parent_uuid"),
	uuidRef("media_assets", "categories.image_id", "image_uuid"),
)

// CategoryModel is the categories row, including nested-set bounds.
type CategoryModel struct {
	ID          int64     `gorm:"primaryKey"`
	UUID        uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	Name        string
	Slug        string
	Description *string
	ParentID    *int64 `gorm:"column:parent_id"`
	ImageID     *int64 `gorm:"column:image_id"`
	Lft         int    `gorm:"column:lft"`
	Rgt         int    `gorm:"column:rgt"`
	Depth       int    `gorm:"column:depth"`
	SortOrder   int    `gorm:"column:sort_order"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ParentUUID  *uuid.UUID `gorm:"column:parent_uuid;->"`
	ImageUUID   *uuid.UUID `gorm:"column:image_uuid;->"`
}

// TableName returns the categories table name for GORM.
func (CategoryModel) TableName() string { return "categories" }

// CategoryRepository implements categoryports.Repository.
type CategoryRepository struct {
	db *gorm.DB
}

// NewCategoryRepository returns a CategoryRepository backed by db.
func NewCategoryRepository(db *gorm.DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

// List returns all categories ordered by nested-set left bound.
func (r *CategoryRepository) List(ctx context.Context) ([]categorydomain.Category, error) {
	var models []CategoryModel
	if err := r.db.WithContext(ctx).Select(categoryColumns).Order("lft ASC").Find(&models).Error; err != nil {
		return nil, err
	}

	out := make([]categorydomain.Category, 0, len(models))
	for _, model := range models {
		out = append(out, mapCategory(model))
	}

	return out, nil
}

// GetByID returns the category with the given UUID or ErrNotFound.
func (r *CategoryRepository) GetByID(ctx context.Context, id uuid.UUID) (categorydomain.Category, error) {
	var model CategoryModel

	err := r.db.WithContext(ctx).Select(categoryColumns).Where("uuid = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}

	if err != nil {
		return categorydomain.Category{}, err
	}

	return mapCategory(model), nil
}

// GetBySlug returns the category with the given slug or ErrNotFound.
func (r *CategoryRepository) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	var model CategoryModel

	err := r.db.WithContext(ctx).Select(categoryColumns).Where("slug = ?", slug).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}

	if err != nil {
		return categorydomain.Category{}, err
	}

	return mapCategory(model), nil
}

// Create inserts a category and returns it with its assigned id.
func (r *CategoryRepository) Create(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	db := r.db.WithContext(ctx)

	parentID, err := optionalIDByUUID(db, "categories", cat.ParentUUID)
	if err != nil {
		return categorydomain.Category{}, err
	}

	imageID, err := optionalIDByUUID(db, "media_assets", cat.ImageUUID)
	if err != nil {
		return categorydomain.Category{}, err
	}

	model := categoryToModel(cat)
	model.ParentID = parentID

	model.ImageID = imageID
	if err := db.Create(&model).Error; err != nil {
		return categorydomain.Category{}, err
	}

	model.ParentUUID = cat.ParentUUID
	model.ImageUUID = cat.ImageUUID

	return mapCategory(model), nil
}

// Update persists name, slug, description, image, and parent changes.
func (r *CategoryRepository) Update(ctx context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	db := r.db.WithContext(ctx)

	parentID, err := optionalIDByUUID(db, "categories", cat.ParentUUID)
	if err != nil {
		return categorydomain.Category{}, err
	}

	imageID, err := optionalIDByUUID(db, "media_assets", cat.ImageUUID)
	if err != nil {
		return categorydomain.Category{}, err
	}

	res := db.Model(&CategoryModel{}).Where("uuid = ?", cat.UUID).
		Updates(map[string]any{
			"name":        cat.Name,
			"slug":        cat.Slug,
			"description": nullableString(cat.Description),
			"parent_id":   parentID,
			"image_id":    imageID,
			"updated_at":  cat.UpdatedAt,
		})
	if res.Error != nil {
		return categorydomain.Category{}, res.Error
	}

	if res.RowsAffected == 0 {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}

	return r.GetByID(ctx, cat.UUID)
}

// Delete removes the category with the given UUID.
func (r *CategoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("uuid = ?", id).Delete(&CategoryModel{})
	if res.Error != nil {
		return res.Error
	}

	if res.RowsAffected == 0 {
		return categorydomain.ErrNotFound
	}

	return nil
}

// SlugTaken reports whether another category (not excludeID) uses slug.
func (r *CategoryRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64

	err := r.db.WithContext(ctx).Model(&CategoryModel{}).
		Where("slug = ? AND uuid <> ?", slug, excludeID).
		Count(&n).Error

	return n > 0, err
}

// CountPosts returns how many posts reference the category.
func (r *CategoryRepository) CountPosts(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64

	err := r.db.WithContext(ctx).Table("posts").Where("category_id = "+idOf("categories"), categoryID).Count(&n).Error

	return n, err
}

// CountChildren returns how many direct children the category has.
func (r *CategoryRepository) CountChildren(ctx context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64

	err := r.db.WithContext(ctx).Model(&CategoryModel{}).Where("parent_id = "+idOf("categories"), categoryID).Count(&n).Error

	return n, err
}

// ReplaceTreeBounds writes recomputed lft/rgt/depth/sort_order for every category.
func (r *CategoryRepository) ReplaceTreeBounds(ctx context.Context, cats []categorydomain.Category) error {
	for _, cat := range cats {
		if err := r.db.WithContext(ctx).Model(&CategoryModel{}).
			Where("uuid = ?", cat.UUID).
			Updates(map[string]any{
				"lft":        cat.Lft,
				"rgt":        cat.Rgt,
				"depth":      cat.Depth,
				"sort_order": cat.SortOrder,
				"parent_id":  parentIDExpr(cat.ParentUUID),
				"updated_at": cat.UpdatedAt,
			}).Error; err != nil {
			return err
		}
	}

	return nil
}

// WithinTx runs fn with a repository bound to a single database transaction.
func (r *CategoryRepository) WithinTx(ctx context.Context, fn func(ctx context.Context, repo categoryports.Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &CategoryRepository{db: tx})
	})
}

func mapCategory(model CategoryModel) categorydomain.Category {
	cat := categorydomain.Category{
		ID:         model.ID,
		UUID:       model.UUID,
		Name:       model.Name,
		Slug:       model.Slug,
		ParentUUID: model.ParentUUID,
		ImageUUID:  model.ImageUUID,
		Lft:        model.Lft,
		Rgt:        model.Rgt,
		Depth:      model.Depth,
		SortOrder:  model.SortOrder,
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  model.UpdatedAt,
	}
	if model.Description != nil {
		cat.Description = *model.Description
	}

	return cat
}

func categoryToModel(cat categorydomain.Category) CategoryModel {
	return CategoryModel{
		ID:          cat.ID,
		UUID:        cat.UUID,
		Name:        cat.Name,
		Slug:        cat.Slug,
		Description: nullableString(cat.Description),
		Lft:         cat.Lft,
		Rgt:         cat.Rgt,
		Depth:       cat.Depth,
		SortOrder:   cat.SortOrder,
		CreatedAt:   cat.CreatedAt,
		UpdatedAt:   cat.UpdatedAt,
	}
}

// parentIDExpr resolves the parent uuid inside the UPDATE, avoiding a lookup per row during tree rebuilds.
func parentIDExpr(parent *uuid.UUID) any {
	if parent == nil {
		return nil
	}

	return gorm.Expr(idOf("categories"), *parent)
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
