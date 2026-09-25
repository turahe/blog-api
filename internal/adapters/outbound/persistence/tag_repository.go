package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	"github.com/turahe/blog-api/internal/core/tag/ports"
	"gorm.io/gorm"
)

var _ ports.Repository = (*TagRepository)(nil)

// TagModel is the tags row.
type TagModel struct {
	ID        int64     `gorm:"primaryKey"`
	UUID      uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	Name      string
	Slug      string
	CreatedAt time.Time
}

// TableName returns the tags table name for GORM.
func (TagModel) TableName() string { return "tags" }

// PostTagModel is the post_tags join row.
type PostTagModel struct {
	PostID int64 `gorm:"primaryKey;autoIncrement:false"`
	TagID  int64 `gorm:"primaryKey;autoIncrement:false"`
}

// TableName returns the post_tags table name for GORM.
func (PostTagModel) TableName() string { return "post_tags" }

// TagRepository implements tagports.Repository.
type TagRepository struct {
	db *gorm.DB
}

// NewTagRepository returns a TagRepository backed by db.
func NewTagRepository(db *gorm.DB) *TagRepository {
	return &TagRepository{db: db}
}

func mapTag(model TagModel) tagdomain.Tag {
	return tagdomain.Tag{
		ID:        model.ID,
		UUID:      model.UUID,
		Name:      model.Name,
		Slug:      model.Slug,
		CreatedAt: model.CreatedAt,
	}
}

// List returns all tags ordered by name.
func (r *TagRepository) List(ctx context.Context) ([]tagdomain.Tag, error) {
	var models []TagModel
	if err := conn(ctx, r.db).Order("name ASC").Find(&models).Error; err != nil {
		return nil, err
	}

	out := make([]tagdomain.Tag, 0, len(models))
	for _, m := range models {
		out = append(out, mapTag(m))
	}

	return out, nil
}

// GetByID returns the tag with the given UUID or ErrNotFound.
func (r *TagRepository) GetByID(ctx context.Context, id uuid.UUID) (tagdomain.Tag, error) {
	var model TagModel

	err := conn(ctx, r.db).Where("uuid = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}

	if err != nil {
		return tagdomain.Tag{}, err
	}

	return mapTag(model), nil
}

// GetBySlug returns the tag with the slug or ErrNotFound.
func (r *TagRepository) GetBySlug(ctx context.Context, slug string) (tagdomain.Tag, error) {
	var model TagModel

	err := conn(ctx, r.db).Where("slug = ?", slug).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}

	if err != nil {
		return tagdomain.Tag{}, err
	}

	return mapTag(model), nil
}

// Create inserts a tag.
func (r *TagRepository) Create(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	model := TagModel{
		UUID:      tag.UUID,
		Name:      tag.Name,
		Slug:      tag.Slug,
		CreatedAt: tag.CreatedAt,
	}
	if err := conn(ctx, r.db).Create(&model).Error; err != nil {
		return tagdomain.Tag{}, err
	}

	return mapTag(model), nil
}

// Update persists tag name and slug changes.
func (r *TagRepository) Update(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	res := conn(ctx, r.db).Model(&TagModel{}).Where("uuid = ?", tag.UUID).
		Updates(map[string]any{
			"name": tag.Name,
			"slug": tag.Slug,
		})
	if res.Error != nil {
		return tagdomain.Tag{}, res.Error
	}

	if res.RowsAffected == 0 {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}

	return r.GetByID(ctx, tag.UUID)
}

// SlugTaken reports whether another tag (not excludeID) uses slug.
func (r *TagRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64

	err := conn(ctx, r.db).Model(&TagModel{}).
		Where("slug = ? AND uuid <> ?", slug, excludeID).
		Count(&n).Error

	return n > 0, err
}

// CountPosts returns how many posts carry the tag.
func (r *TagRepository) CountPosts(ctx context.Context, tagID uuid.UUID) (int64, error) {
	var n int64

	err := conn(ctx, r.db).Model(&PostTagModel{}).
		Where("tag_id = "+idOf("tags"), tagID).
		Count(&n).Error

	return n, err
}

// MergeInto moves source's post links to target and deletes source, in one transaction.
func (r *TagRepository) MergeInto(ctx context.Context, sourceID, targetID uuid.UUID) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		sourceRowID, err := idByUUID(tx, "tags", sourceID)
		if errors.Is(err, errUnknownReference) {
			return tagdomain.ErrNotFound
		}

		if err != nil {
			return err
		}

		targetRowID, err := idByUUID(tx, "tags", targetID)
		if err != nil {
			return err
		}

		if err := tx.Exec(`
			INSERT INTO post_tags (post_id, tag_id)
			SELECT post_id, ? FROM post_tags WHERE tag_id = ?
			ON CONFLICT DO NOTHING`, targetRowID, sourceRowID).Error; err != nil {
			return err
		}

		if err := tx.Where("tag_id = ?", sourceRowID).Delete(&PostTagModel{}).Error; err != nil {
			return err
		}

		res := tx.Where("id = ?", sourceRowID).Delete(&TagModel{})
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return tagdomain.ErrNotFound
		}

		return nil
	})
}

// Delete removes the tag with the given UUID.
func (r *TagRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res := conn(ctx, r.db).Where("uuid = ?", id).Delete(&TagModel{})
	if res.Error != nil {
		return res.Error
	}

	if res.RowsAffected == 0 {
		return tagdomain.ErrNotFound
	}

	return nil
}

// ReplacePostTags sets the post's tags to exactly tagIDs.
func (r *TagRepository) ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		postRowID, err := idByUUID(tx, "posts", postID)
		if err != nil {
			return err
		}

		if err := tx.Where("post_id = ?", postRowID).Delete(&PostTagModel{}).Error; err != nil {
			return err
		}

		if len(tagIDs) == 0 {
			return nil
		}

		var tagRowIDs []int64
		if err := tx.Model(&TagModel{}).Where("uuid IN ?", tagIDs).Pluck("id", &tagRowIDs).Error; err != nil {
			return err
		}

		distinct := make(map[uuid.UUID]struct{}, len(tagIDs))
		for _, tagID := range tagIDs {
			distinct[tagID] = struct{}{}
		}

		if len(tagRowIDs) != len(distinct) {
			return fmt.Errorf("post tags: %w", errUnknownReference)
		}

		models := make([]PostTagModel, 0, len(tagRowIDs))
		for _, tagRowID := range tagRowIDs {
			models = append(models, PostTagModel{PostID: postRowID, TagID: tagRowID})
		}

		return tx.Create(&models).Error
	})
}

// ListByPostID returns the post's tags ordered by name.
func (r *TagRepository) ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error) {
	var models []TagModel

	err := conn(ctx, r.db).
		Model(&TagModel{}).
		Joins("INNER JOIN post_tags ON post_tags.tag_id = tags.id").
		Where("post_tags.post_id = "+idOf("posts"), postID).
		Order("tags.name ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}

	out := make([]tagdomain.Tag, 0, len(models))
	for _, m := range models {
		out = append(out, mapTag(m))
	}

	return out, nil
}
