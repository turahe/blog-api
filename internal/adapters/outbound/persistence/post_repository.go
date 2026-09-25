package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

var postColumns = withRefs("posts",
	uuidRef("users", "posts.author_id", "author_uuid"),
	uuidRef("categories", "posts.category_id", "category_uuid"),
	uuidRef("media_assets", "posts.cover_image_media_id", "cover_image_media_uuid"),
)

// PostRepository implements postports.Repository.
type PostRepository struct {
	db *gorm.DB
}

// NewPostRepository returns a PostRepository backed by db.
func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{db: db}
}

// ListPublished returns a page of published, non-deleted posts.
func (r *PostRepository) ListPublished(ctx context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error) {
	q := conn(ctx, r.db).Model(&PostModel{}).Where("status = ? AND deleted_at IS NULL", string(postdomain.StatusPublished))
	if filter.CategoryUUID != nil {
		q = q.Where("category_id = "+idOf("categories"), *filter.CategoryUUID)
	}

	if filter.TagUUID != nil {
		q = q.Where("id IN (SELECT post_id FROM post_tags WHERE tag_id = "+idOf("tags")+")", *filter.TagUUID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	var models []PostModel

	offset := (filter.Page - 1) * filter.PerPage
	if err := q.Select(postColumns).Order("published_at DESC NULLS LAST, created_at DESC, id DESC").
		Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	items := make([]postdomain.Post, 0, len(models))
	for _, model := range models {
		items = append(items, mapPost(model))
	}

	return postdomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

// ListAdmin returns a page of posts for the admin list, filtered by status, author, and query.
func (r *PostRepository) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	q := conn(ctx, r.db).Model(&PostModel{}).Where("deleted_at IS NULL")
	if filter.Trashed {
		q = conn(ctx, r.db).Unscoped().Model(&PostModel{}).Where("deleted_at IS NOT NULL")
	}

	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}

	if filter.AuthorUUID != nil {
		q = q.Where("author_id = "+idOf("users"), *filter.AuthorUUID)
	}

	if filter.CategoryUUID != nil {
		q = q.Where("category_id = "+idOf("categories"), *filter.CategoryUUID)
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
	if err := q.Select(postColumns).Order("created_at DESC, id DESC").
		Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return postdomain.ListResult{}, err
	}

	items := make([]postdomain.Post, 0, len(models))
	for _, model := range models {
		items = append(items, mapPost(model))
	}

	return postdomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

// GetPublishedBySlug returns the published post with the slug or ErrNotFound.
func (r *PostRepository) GetPublishedBySlug(ctx context.Context, slug string) (postdomain.Post, error) {
	var model PostModel

	err := conn(ctx, r.db).Select(postColumns).
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

// GetByID returns the live post with the given UUID or ErrNotFound.
func (r *PostRepository) GetByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	return r.getByID(conn(ctx, r.db), id)
}

// GetDeletedByID returns the soft-deleted post with the given UUID or ErrNotFound.
func (r *PostRepository) GetDeletedByID(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
	return r.getByID(conn(ctx, r.db).Unscoped().Where("deleted_at IS NOT NULL"), id)
}

func (r *PostRepository) getByID(db *gorm.DB, id uuid.UUID) (postdomain.Post, error) {
	var model PostModel

	err := db.Select(postColumns).Where("uuid = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return postdomain.Post{}, postdomain.ErrNotFound
	}

	if err != nil {
		return postdomain.Post{}, err
	}

	return mapPost(model), nil
}

// Create inserts a post and returns it with resolved references.
func (r *PostRepository) Create(ctx context.Context, post postdomain.Post) (postdomain.Post, error) {
	db := conn(ctx, r.db)

	authorID, err := idByUUID(db, "users", post.AuthorUUID)
	if err != nil {
		return postdomain.Post{}, err
	}

	categoryID, err := optionalIDByUUID(db, "categories", post.CategoryUUID)
	if err != nil {
		return postdomain.Post{}, invalidReference(err, postdomain.ErrValidation, "category_id")
	}

	coverID, err := optionalIDByUUID(db, "media_assets", post.CoverImageMediaUUID)
	if err != nil {
		return postdomain.Post{}, invalidReference(err, postdomain.ErrValidation, "cover_image_media_id")
	}

	model := PostModel{
		UUID: post.UUID, AuthorID: authorID, CategoryID: categoryID,
		Title: post.Title, Slug: post.Slug, Content: post.Content,
		CoverImageMediaID: coverID,
		Status:            string(post.Status), CommentPolicy: string(post.CommentPolicy), Version: post.Version,
		PublishedAt: post.PublishedAt, CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt,
	}
	if post.Excerpt != "" {
		model.Excerpt = &post.Excerpt
	}

	if err := db.Create(&model).Error; err != nil {
		return postdomain.Post{}, slugConflict(err)
	}

	model.AuthorUUID = post.AuthorUUID
	model.CategoryUUID = post.CategoryUUID
	model.CoverImageMediaUUID = post.CoverImageMediaUUID

	return mapPost(model), nil
}

// Update persists post fields and returns the stored row.
func (r *PostRepository) Update(ctx context.Context, post postdomain.Post) (postdomain.Post, error) {
	db := conn(ctx, r.db)

	categoryID, err := optionalIDByUUID(db, "categories", post.CategoryUUID)
	if err != nil {
		return postdomain.Post{}, invalidReference(err, postdomain.ErrValidation, "category_id")
	}

	coverID, err := optionalIDByUUID(db, "media_assets", post.CoverImageMediaUUID)
	if err != nil {
		return postdomain.Post{}, invalidReference(err, postdomain.ErrValidation, "cover_image_media_id")
	}

	updates := map[string]any{
		"title":                post.Title,
		"slug":                 post.Slug,
		"content":              post.Content,
		"status":               string(post.Status),
		"comment_policy":       string(commentPolicyOrOpen(post.CommentPolicy)),
		"version":              post.Version,
		"published_at":         post.PublishedAt,
		"updated_at":           post.UpdatedAt,
		"category_id":          categoryID,
		"cover_image_media_id": coverID,
	}
	if post.Excerpt == "" {
		updates["excerpt"] = nil
	} else {
		updates["excerpt"] = post.Excerpt
	}

	res := db.Model(&PostModel{}).
		Where("uuid = ? AND version = ?", post.UUID, post.Version-1).
		Updates(updates)
	if res.Error != nil {
		return postdomain.Post{}, slugConflict(res.Error)
	}

	if res.RowsAffected == 0 {
		if _, err := r.GetByID(ctx, post.UUID); err != nil {
			return postdomain.Post{}, err
		}

		return postdomain.Post{}, postdomain.ErrStaleVersion
	}

	return r.GetByID(ctx, post.UUID)
}

// SoftDelete sets deleted_at on a live post whose stored version is post.Version-1.
func (r *PostRepository) SoftDelete(ctx context.Context, post postdomain.Post) error {
	res := conn(ctx, r.db).Model(&PostModel{}).
		Where("uuid = ? AND version = ?", post.UUID, post.Version-1).
		Updates(map[string]any{
			"deleted_at": post.DeletedAt,
			"updated_at": post.UpdatedAt,
			"version":    post.Version,
		})
	if res.Error != nil {
		return res.Error
	}

	if res.RowsAffected == 0 {
		if _, err := r.GetByID(ctx, post.UUID); err != nil {
			return err
		}

		return postdomain.ErrStaleVersion
	}

	return nil
}

// Restore revives a soft-deleted post whose stored version is post.Version-1 with the
// post's slug, status, and published_at.
func (r *PostRepository) Restore(ctx context.Context, post postdomain.Post) (postdomain.Post, error) {
	res := conn(ctx, r.db).Unscoped().Model(&PostModel{}).
		Where("uuid = ? AND version = ? AND deleted_at IS NOT NULL", post.UUID, post.Version-1).
		Updates(map[string]any{
			"deleted_at":   nil,
			"slug":         post.Slug,
			"status":       string(post.Status),
			"published_at": post.PublishedAt,
			"updated_at":   post.UpdatedAt,
			"version":      post.Version,
		})
	if res.Error != nil {
		return postdomain.Post{}, slugConflict(res.Error)
	}

	if res.RowsAffected == 0 {
		if _, err := r.GetDeletedByID(ctx, post.UUID); err != nil {
			return postdomain.Post{}, err
		}

		return postdomain.Post{}, postdomain.ErrStaleVersion
	}

	return r.GetByID(ctx, post.UUID)
}

// SlugsWithPrefix returns live slugs equal to base or beginning with base + "-".
func (r *PostRepository) SlugsWithPrefix(ctx context.Context, base string) ([]string, error) {
	var slugs []string

	err := conn(ctx, r.db).Model(&PostModel{}).
		Where("slug = ? OR left(slug, ?) = ?", base, len(base)+1, base+"-").
		Pluck("slug", &slugs).Error

	return slugs, err
}

// SlugTaken reports whether another post (not excludeID) uses slug.
func (r *PostRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64

	err := conn(ctx, r.db).Model(&PostModel{}).
		Where("slug = ? AND uuid <> ? AND deleted_at IS NULL", slug, excludeID).
		Count(&n).Error

	return n > 0, err
}

// SetCoverImage points the post's cover at mediaID, or clears it when nil.
func (r *PostRepository) SetCoverImage(ctx context.Context, postID uuid.UUID, mediaID *uuid.UUID, updatedAt time.Time) error {
	db := conn(ctx, r.db)

	coverID, err := optionalIDByUUID(db, "media_assets", mediaID)
	if err != nil {
		return invalidReference(err, postdomain.ErrValidation, "cover_image_media_id")
	}

	res := db.Model(&PostModel{}).
		Where("uuid = ? AND deleted_at IS NULL", postID).
		Updates(map[string]any{
			"cover_image_media_id": coverID,
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

// slugConflict maps a violation of the live-slug unique index to ErrConflict.
func slugConflict(err error) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: slug already in use", postdomain.ErrConflict)
	}

	return err
}

func commentPolicyOrOpen(policy postdomain.CommentPolicy) postdomain.CommentPolicy {
	if policy == "" {
		return postdomain.CommentPolicyOpen
	}

	return policy
}

func mapPost(model PostModel) postdomain.Post {
	post := postdomain.Post{
		ID:                  model.ID,
		UUID:                model.UUID,
		AuthorUUID:          model.AuthorUUID,
		CategoryUUID:        model.CategoryUUID,
		Title:               model.Title,
		Slug:                model.Slug,
		Content:             model.Content,
		CoverImageMediaUUID: model.CoverImageMediaUUID,
		Status:              postdomain.Status(model.Status),
		CommentPolicy:       commentPolicyOrOpen(postdomain.CommentPolicy(model.CommentPolicy)),
		Version:             model.Version,
		PublishedAt:         model.PublishedAt,
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           model.UpdatedAt,
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
