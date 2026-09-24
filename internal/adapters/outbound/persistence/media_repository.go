package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/media/ports"
	"gorm.io/gorm"
)

var _ ports.Repository = (*MediaRepository)(nil)

var mediaColumns = withRefs("media_assets", uuidRef("users", "media_assets.uploaded_by", "uploaded_by_uuid"))

type MediaRepository struct {
	db *gorm.DB
}

func NewMediaRepository(db *gorm.DB) *MediaRepository {
	return &MediaRepository{db: db}
}

func (r *MediaRepository) Create(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	now := time.Now().UTC()
	if asset.CreatedAt.IsZero() {
		asset.CreatedAt = now
	}
	if asset.UpdatedAt.IsZero() {
		asset.UpdatedAt = asset.CreatedAt
	}

	db := r.db.WithContext(ctx)
	uploadedBy, err := optionalIDByUUID(db, "users", asset.UploadedByUUID)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}
	model := mapMediaAssetModel(asset)
	model.UploadedBy = uploadedBy
	if err := db.Create(&model).Error; err != nil {
		return mediadomain.MediaAsset{}, err
	}
	model.UploadedByUUID = asset.UploadedByUUID

	return mapMediaAsset(model), nil
}

func (r *MediaRepository) GetByID(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	var model MediaAssetModel
	err := r.db.WithContext(ctx).
		Select(mediaColumns).
		Where("uuid = ? AND deleted_at IS NULL", id).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediadomain.MediaAsset{}, mediadomain.ErrNotFound
	}
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	return mapMediaAsset(model), nil
}

func (r *MediaRepository) Update(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	if asset.UpdatedAt.IsZero() {
		asset.UpdatedAt = time.Now().UTC()
	}
	db := r.db.WithContext(ctx)
	uploadedBy, err := optionalIDByUUID(db, "users", asset.UploadedByUUID)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	updates := map[string]any{
		"storage_key":        asset.StorageKey,
		"original_filename":  asset.OriginalFilename,
		"content_type":       asset.ContentType,
		"size_bytes":         asset.SizeBytes,
		"width":              asset.Width,
		"height":             asset.Height,
		"checksum_sha256":    asset.ChecksumSHA256,
		"disk":               asset.Disk,
		"status":             asset.Status,
		"uploaded_by":        uploadedBy,
		"tags":               mediaTags(asset.Tags),
		"presign_expires_at": asset.PresignExpiresAt,
		"updated_at":         asset.UpdatedAt,
		"deleted_at":         asset.DeletedAt,
	}

	if err := db.
		Model(&MediaAssetModel{}).
		Where("uuid = ? AND deleted_at IS NULL", asset.UUID).
		Updates(updates).Error; err != nil {
		return mediadomain.MediaAsset{}, err
	}

	return r.GetByID(ctx, asset.UUID)
}

func (r *MediaRepository) List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	q := r.db.WithContext(ctx).Model(&MediaAssetModel{}).Where("deleted_at IS NULL")
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		q = q.Where("original_filename ILIKE ? OR storage_key ILIKE ?", like, like)
	}
	if filter.Disk != "" {
		q = q.Where("disk = ?", filter.Disk)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return mediadomain.ListResult{}, err
	}

	var models []MediaAssetModel
	offset := (filter.Page - 1) * filter.PerPage
	if err := q.Select(mediaColumns).Order("created_at DESC").Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return mediadomain.ListResult{}, err
	}

	items := make([]mediadomain.MediaAsset, 0, len(models))
	for _, model := range models {
		items = append(items, mapMediaAsset(model))
	}
	return mediadomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *MediaRepository) SoftDelete(ctx context.Context, id uuid.UUID, deletedAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&MediaAssetModel{}).
		Where("uuid = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"deleted_at": gorm.DeletedAt{Time: deletedAt, Valid: true},
			"updated_at": deletedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return mediadomain.ErrNotFound
	}
	return nil
}

func (r *MediaRepository) ClearEntityReferences(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mediaID, err := idByUUID(tx, "media_assets", id)
		if errors.Is(err, errUnknownReference) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE users SET avatar_id = NULL WHERE avatar_id = ?`, mediaID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE categories SET image_id = NULL WHERE image_id = ?`, mediaID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE posts SET cover_image_media_id = NULL WHERE cover_image_media_id = ?`, mediaID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM post_media WHERE media_asset_id = ?`, mediaID).Error; err != nil {
			return err
		}
		return nil
	})
}

func mapMediaAssetModel(asset mediadomain.MediaAsset) MediaAssetModel {
	model := MediaAssetModel{
		ID:               asset.ID,
		UUID:             asset.UUID,
		StorageKey:       asset.StorageKey,
		OriginalFilename: asset.OriginalFilename,
		ContentType:      asset.ContentType,
		SizeBytes:        asset.SizeBytes,
		Width:            asset.Width,
		Height:           asset.Height,
		ChecksumSHA256:   asset.ChecksumSHA256,
		Disk:             asset.Disk,
		Status:           asset.Status,
		Tags:             mediaTags(asset.Tags),
		PresignExpiresAt: asset.PresignExpiresAt,
		CreatedAt:        asset.CreatedAt,
		UpdatedAt:        asset.UpdatedAt,
	}
	if asset.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *asset.DeletedAt, Valid: true}
	}

	return model
}

func mapMediaAsset(model MediaAssetModel) mediadomain.MediaAsset {
	asset := mediadomain.MediaAsset{
		ID:               model.ID,
		UUID:             model.UUID,
		StorageKey:       model.StorageKey,
		OriginalFilename: model.OriginalFilename,
		ContentType:      model.ContentType,
		SizeBytes:        model.SizeBytes,
		Width:            model.Width,
		Height:           model.Height,
		ChecksumSHA256:   model.ChecksumSHA256,
		Disk:             model.Disk,
		Status:           model.Status,
		UploadedByUUID:   model.UploadedByUUID,
		Tags:             append([]string(nil), []string(model.Tags)...),
		PresignExpiresAt: model.PresignExpiresAt,
		CreatedAt:        model.CreatedAt,
		UpdatedAt:        model.UpdatedAt,
	}
	if model.DeletedAt.Valid {
		t := model.DeletedAt.Time
		asset.DeletedAt = &t
	}

	return asset
}

// mediaTags never returns nil: pq encodes a nil array as NULL, which the NOT NULL tags column rejects.
func mediaTags(tags []string) pq.StringArray {
	out := make(pq.StringArray, len(tags))
	copy(out, tags)
	return out
}
