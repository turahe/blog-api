package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

type PostRepository struct {
	db *gorm.DB
}

func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{db: db}
}

func (r *PostRepository) ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error) {
	q := r.db.WithContext(ctx).Model(&PostModel{}).Where("status = ? AND deleted_at IS NULL", string(postdomain.StatusPublished))
	if filter.CategoryID != nil {
		q = q.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.TagID != nil {
		q = q.Where("id IN (SELECT post_id FROM post_tags WHERE tag_id = ?)", *filter.TagID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	var models []PostModel
	offset := (filter.Page - 1) * filter.PerPage
	if err := q.Order("published_at DESC NULLS LAST, created_at DESC").
		Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	items := make([]postdomain.Post, 0, len(models))
	for _, model := range models {
		items = append(items, mapPost(model))
	}
	return postdomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *PostRepository) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	q := r.db.WithContext(ctx).Model(&PostModel{}).Where("deleted_at IS NULL")
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.AuthorID != nil {
		q = q.Where("author_id = ?", *filter.AuthorID)
	}
	if filter.CategoryID != nil {
		q = q.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		q = q.Where("title ILIKE ? OR slug ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	var models []PostModel
	offset := (filter.Page - 1) * filter.PerPage
	if err := q.Order("created_at DESC").
		Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	items := make([]postdomain.Post, 0, len(models))
	for _, model := range models {
		items = append(items, mapPost(model))
	}
	return postdomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *PostRepository) GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error) {
	var model PostModel
	err := r.db.WithContext(ctx).
		Where("slug = ? AND status = ? AND deleted_at IS NULL", slug, string(postdomain.StatusPublished)).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	if err != nil {
		return postdomain.Post{}, err
	}
	return mapPost(model), nil
}

func (r *PostRepository) GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	var model PostModel
	err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	if err != nil {
		return postdomain.Post{}, err
	}
	return mapPost(model), nil
}

func (r *PostRepository) Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error) {
	model := PostModel{
		ID: post.ID, AuthorID: post.AuthorID, CategoryID: post.CategoryID,
		Title: post.Title, Slug: post.Slug, Content: post.Content,
		CoverImageMediaID: post.CoverImageMediaID,
		Status: string(post.Status), Version: post.Version,
		PublishedAt: post.PublishedAt, CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt,
	}
	if post.Excerpt != "" {
		model.Excerpt = &post.Excerpt
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return postdomain.Post{}, err
	}
	return mapPost(model), nil
}

func (r *PostRepository) Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error) {
	updates := map[string]any{
		"title":                post.Title,
		"slug":                 post.Slug,
		"content":              post.Content,
		"status":               string(post.Status),
		"version":              post.Version,
		"published_at":         post.PublishedAt,
		"updated_at":           post.UpdatedAt,
		"category_id":          post.CategoryID,
		"cover_image_media_id": post.CoverImageMediaID,
	}
	if post.Excerpt == "" {
		updates["excerpt"] = nil
	} else {
		updates["excerpt"] = post.Excerpt
	}
	if err := r.db.WithContext(ctx).Model(&PostModel{}).Where("id = ?", post.ID).Updates(updates).Error; err != nil {
		return postdomain.Post{}, err
	}
	return r.GetByID(ctx, post.ID)
}

func (r *PostRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&PostModel{}).
		Where("slug = ? AND id <> ? AND deleted_at IS NULL", slug, excludeID).
		Count(&n).Error
	return n > 0, err
}

func (r *PostRepository) SetCoverImage(ctx context.Context, postID uuid.UUID, mediaID *uuid.UUID, updatedAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&PostModel{}).
		Where("id = ? AND deleted_at IS NULL", postID).
		Updates(map[string]any{
			"cover_image_media_id": mediaID,
			"updated_at":           updatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return postdomain.ErrNotFound
	}
	return nil
}

func mapPost(model PostModel) postdomain.Post {
	post := postdomain.Post{
		ID:                model.ID,
		AuthorID:          model.AuthorID,
		CategoryID:        model.CategoryID,
		Title:             model.Title,
		Slug:              model.Slug,
		Content:           model.Content,
		CoverImageMediaID: model.CoverImageMediaID,
		Status:            postdomain.Status(model.Status),
		Version:           model.Version,
		PublishedAt:       model.PublishedAt,
		CreatedAt:         model.CreatedAt,
		UpdatedAt:         model.UpdatedAt,
	}
	if model.Excerpt != nil {
		post.Excerpt = *model.Excerpt
	}
	if model.DeletedAt.Valid {
		t := model.DeletedAt.Time
		post.DeletedAt = &t
	}
	return post
}

