package persistence

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
	"gorm.io/gorm"
)

var _ ports.PostMediaRepository = (*PostMediaRepository)(nil)

type PostMediaModel struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	PostID       uuid.UUID `gorm:"type:uuid;column:post_id"`
	MediaAssetID uuid.UUID `gorm:"type:uuid;column:media_asset_id"`
	Kind         string    `gorm:"column:kind"`
	SortOrder    int       `gorm:"column:sort_order"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (PostMediaModel) TableName() string { return "post_media" }

type PostMediaRepository struct {
	db *gorm.DB
}

func NewPostMediaRepository(db *gorm.DB) *PostMediaRepository {
	return &PostMediaRepository{db: db}
}

func (r *PostMediaRepository) ReplaceAll(ctx context.Context, postID uuid.UUID, items []mediadomain.PostMediaItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", postID).Delete(&PostMediaModel{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		now := time.Now().UTC()
		models := make([]PostMediaModel, 0, len(items))
		for _, item := range items {
			models = append(models, PostMediaModel{
				ID:           uuid.New(),
				PostID:       postID,
				MediaAssetID: item.MediaAssetID,
				Kind:         item.Kind,
				SortOrder:    item.SortOrder,
				CreatedAt:    now,
			})
		}
		return tx.Create(&models).Error
	})
}

func (r *PostMediaRepository) ListByPostID(ctx context.Context, postID uuid.UUID) ([]mediadomain.PostMediaItem, error) {
	var models []PostMediaModel
	if err := r.db.WithContext(ctx).
		Where("post_id = ?", postID).
		Order("sort_order ASC, created_at ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]mediadomain.PostMediaItem, 0, len(models))
	for _, model := range models {
		items = append(items, mediadomain.PostMediaItem{
			MediaAssetID: model.MediaAssetID,
			Kind:         model.Kind,
			SortOrder:    model.SortOrder,
		})
	}
	return items, nil
}
