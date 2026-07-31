package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	"github.com/turahe/blog-api/internal/core/tag/ports"
	"gorm.io/gorm"
)

var _ ports.Repository = (*TagRepository)(nil)

type TagModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name      string
	Slug      string
	CreatedAt time.Time
}

func (TagModel) TableName() string { return "tags" }

type PostTagModel struct {
	PostID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TagID  uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (PostTagModel) TableName() string { return "post_tags" }

type TagRepository struct {
	db *gorm.DB
}

func NewTagRepository(db *gorm.DB) *TagRepository {
	return &TagRepository{db: db}
}

func mapTag(model TagModel) tagdomain.Tag {
	return tagdomain.Tag{
		ID:        model.ID,
		Name:      model.Name,
		Slug:      model.Slug,
		CreatedAt: model.CreatedAt,
	}
}

func (r *TagRepository) List(ctx context.Context) ([]tagdomain.Tag, error) {
	var models []TagModel
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]tagdomain.Tag, 0, len(models))
	for _, m := range models {
		out = append(out, mapTag(m))
	}
	return out, nil
}

func (r *TagRepository) GetByID(ctx context.Context, id uuid.UUID) (tagdomain.Tag, error) {
	var model TagModel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}
	if err != nil {
		return tagdomain.Tag{}, err
	}
	return mapTag(model), nil
}

func (r *TagRepository) GetBySlug(ctx context.Context, slug string) (tagdomain.Tag, error) {
	var model TagModel
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}
	if err != nil {
		return tagdomain.Tag{}, err
	}
	return mapTag(model), nil
}

func (r *TagRepository) Create(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	model := TagModel{
		ID:        tag.ID,
		Name:      tag.Name,
		Slug:      tag.Slug,
		CreatedAt: tag.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return tagdomain.Tag{}, err
	}
	return mapTag(model), nil
}

func (r *TagRepository) Update(ctx context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	res := r.db.WithContext(ctx).Model(&TagModel{}).Where("id = ?", tag.ID).
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
	return r.GetByID(ctx, tag.ID)
}

func (r *TagRepository) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&TagModel{}).
		Where("slug = ? AND id <> ?", slug, excludeID).
		Count(&n).Error
	return n > 0, err
}

func (r *TagRepository) CountPosts(ctx context.Context, tagID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&PostTagModel{}).
		Where("tag_id = ?", tagID).
		Count(&n).Error
	return n, err
}

func (r *TagRepository) MergeInto(ctx context.Context, sourceID, targetID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO post_tags (post_id, tag_id)
			SELECT post_id, ? FROM post_tags WHERE tag_id = ?
			ON CONFLICT DO NOTHING`, targetID, sourceID).Error; err != nil {
			return err
		}
		if err := tx.Where("tag_id = ?", sourceID).Delete(&PostTagModel{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", sourceID).Delete(&TagModel{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return tagdomain.ErrNotFound
		}
		return nil
	})
}

func (r *TagRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&TagModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tagdomain.ErrNotFound
	}
	return nil
}

func (r *TagRepository) ReplacePostTags(ctx context.Context, postID uuid.UUID, tagIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", postID).Delete(&PostTagModel{}).Error; err != nil {
			return err
		}
		if len(tagIDs) == 0 {
			return nil
		}
		models := make([]PostTagModel, 0, len(tagIDs))
		for _, tagID := range tagIDs {
			models = append(models, PostTagModel{PostID: postID, TagID: tagID})
		}
		return tx.Create(&models).Error
	})
}

func (r *TagRepository) ListByPostID(ctx context.Context, postID uuid.UUID) ([]tagdomain.Tag, error) {
	var models []TagModel
	err := r.db.WithContext(ctx).
		Model(&TagModel{}).
		Joins("INNER JOIN post_tags ON post_tags.tag_id = tags.id").
		Where("post_tags.post_id = ?", postID).
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
