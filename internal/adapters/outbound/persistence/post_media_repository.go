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
	ID             int64     `gorm:"primaryKey"`
	UUID           uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	PostID         int64     `gorm:"column:post_id"`
	MediaAssetID   int64     `gorm:"column:media_asset_id"`
	Kind           string    `gorm:"column:kind"`
	SortOrder      int       `gorm:"column:sort_order"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	MediaAssetUUID uuid.UUID `gorm:"column:media_asset_uuid;->"`
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
		postRowID, err := idByUUID(tx, "posts", postID)
		if err != nil {
			return err
		}
		if err := tx.Where("post_id = ?", postRowID).Delete(&PostMediaModel{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		now := time.Now().UTC()
		models := make([]PostMediaModel, 0, len(items))
		for _, item := range items {
			mediaID, err := idByUUID(tx, "media_assets", item.MediaAssetUUID)
			if err != nil {
				return err
			}
			models = append(models, PostMediaModel{
				UUID:         uuid.New(),
				PostID:       postRowID,
				MediaAssetID: mediaID,
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
		Select(withRefs("post_media", uuidRef("media_assets", "post_media.media_asset_id", "media_asset_uuid"))).
		Where("post_id = "+idOf("posts"), postID).
		Order("sort_order ASC, created_at ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]mediadomain.PostMediaItem, 0, len(models))
	for _, model := range models {
		items = append(items, mediadomain.PostMediaItem{
			MediaAssetUUID: model.MediaAssetUUID,
			Kind:           model.Kind,
			SortOrder:      model.SortOrder,
		})
	}
	return items, nil
}
