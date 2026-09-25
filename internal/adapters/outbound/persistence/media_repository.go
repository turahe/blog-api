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

// MediaRepository implements mediaports.Repository.
type MediaRepository struct {
	db *gorm.DB
}

// NewMediaRepository returns a MediaRepository backed by db.
func NewMediaRepository(db *gorm.DB) *MediaRepository {
	return &MediaRepository{db: db}
}

// Create inserts a media asset row.
func (r *MediaRepository) Create(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	now := time.Now().UTC()
	if asset.CreatedAt.IsZero() {
		asset.CreatedAt = now
	}

	if asset.UpdatedAt.IsZero() {
		asset.UpdatedAt = asset.CreatedAt
	}

	db := conn(ctx, r.db)

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

// GetByID returns the live media asset with the given UUID or ErrNotFound.
func (r *MediaRepository) GetByID(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	var model MediaAssetModel

	err := conn(ctx, r.db).
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

// Update persists status, size, checksum, and tag changes.
func (r *MediaRepository) Update(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error) {
	if asset.UpdatedAt.IsZero() {
		asset.UpdatedAt = time.Now().UTC()
	}

	db := conn(ctx, r.db)

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

// List returns a page of live media assets matching the filter.
func (r *MediaRepository) List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	q := conn(ctx, r.db).Model(&MediaAssetModel{}).Where("deleted_at IS NULL")

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

	if filter.Unused {
		q = q.Where("status = ? AND "+mediaUnreferenced, mediadomain.StatusReady)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return mediadomain.ListResult{}, err
	}

	var models []MediaAssetModel

	offset := (filter.Page - 1) * filter.PerPage
	if err := q.Select(mediaColumns).Order("created_at DESC, id DESC").Limit(filter.PerPage).Offset(offset).Find(&models).Error; err != nil {
		return mediadomain.ListResult{}, err
	}

	items := make([]mediadomain.MediaAsset, 0, len(models))
	for _, model := range models {
		items = append(items, mapMediaAsset(model))
	}

	return mediadomain.ListResult{Items: items, Total: total, Page: filter.Page, PerPage: filter.PerPage}, nil
}

// SoftDelete marks the asset deleted at deletedAt.
func (r *MediaRepository) SoftDelete(ctx context.Context, id uuid.UUID, deletedAt time.Time) error {
	res := conn(ctx, r.db).Model(&MediaAssetModel{}).
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

// ClearEntityReferences nulls user avatar, category image, and post cover references to the asset.
func (r *MediaRepository) ClearEntityReferences(ctx context.Context, id uuid.UUID) error {
	return conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
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

// mediaUnreferenced matches assets no row points at. Post bodies are not parsed.
const mediaUnreferenced = `NOT EXISTS (SELECT 1 FROM users u WHERE u.avatar_id = media_assets.id)
	AND NOT EXISTS (SELECT 1 FROM categories c WHERE c.image_id = media_assets.id)
	AND NOT EXISTS (SELECT 1 FROM posts p WHERE p.cover_image_media_id = media_assets.id)
	AND NOT EXISTS (SELECT 1 FROM post_media pm WHERE pm.media_asset_id = media_assets.id)
	AND NOT EXISTS (SELECT 1 FROM post_seo ps WHERE ps.og_image_id = media_assets.id OR ps.twitter_image_id = media_assets.id)`

// Orphan conditions; Purge re-checks them so a row that changed since it was listed stays.
const (
	mediaAbandoned = `deleted_at IS NULL AND status <> 'ready' AND presign_expires_at < ?`
	mediaTrashed   = `deleted_at IS NOT NULL AND deleted_at < ?`
)

// Abandoned returns live assets that never became ready and whose presign expired before
// expiredBefore, oldest first.
func (r *MediaRepository) Abandoned(ctx context.Context, expiredBefore time.Time, limit int) ([]mediadomain.MediaAsset, error) {
	return r.orphans(ctx, mediaAbandoned, expiredBefore, "presign_expires_at", limit)
}

// Trashed returns assets soft-deleted before deletedBefore, oldest first.
func (r *MediaRepository) Trashed(ctx context.Context, deletedBefore time.Time, limit int) ([]mediadomain.MediaAsset, error) {
	return r.orphans(ctx, mediaTrashed, deletedBefore, "deleted_at", limit)
}

func (r *MediaRepository) orphans(ctx context.Context, where string, before time.Time, order string, limit int) ([]mediadomain.MediaAsset, error) {
	var models []MediaAssetModel

	err := conn(ctx, r.db).Unscoped().Select(mediaColumns).
		Where(where, before).Order(order + ", id").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}

	out := make([]mediadomain.MediaAsset, 0, len(models))
	for _, m := range models {
		out = append(out, mapMediaAsset(m))
	}

	return out, nil
}

// Purge deletes the row while it is still soft-deleted or an upload whose presign expired
// (which can no longer complete). References are ON DELETE SET NULL or CASCADE.
func (r *MediaRepository) Purge(ctx context.Context, id uuid.UUID) (bool, error) {
	res := conn(ctx, r.db).Exec(`DELETE FROM media_assets WHERE uuid = ? AND (deleted_at IS NOT NULL
		OR (status <> 'ready' AND presign_expires_at < now()))`, id)

	return res.RowsAffected > 0, res.Error
}

type usageRow struct {
	Key   string
	Count int64
	Bytes int64
}

type uploaderRow struct {
	UserUUID *uuid.UUID
	Username *string
	Count    int64
	Bytes    int64
}

// Usage groups every stored asset, soft-deleted ones under "deleted", by status, content type,
// and uploader.
func (r *MediaRepository) Usage(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error) {
	db := conn(ctx, r.db)
	scope := "TRUE"

	var args []any

	if filter.UploadedBy != nil {
		scope, args = "m.uploaded_by = (SELECT id FROM users WHERE uuid = ?)", []any{*filter.UploadedBy}
	}

	group := func(key string) ([]mediadomain.UsageRow, error) {
		var rows []usageRow

		err := db.Raw(`SELECT `+key+` AS key, count(*) AS count, coalesce(sum(m.size_bytes), 0) AS bytes
			FROM media_assets m WHERE `+scope+` GROUP BY 1 ORDER BY 3 DESC, 1`, args...).Scan(&rows).Error

		out := make([]mediadomain.UsageRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, mediadomain.UsageRow(row))
		}

		return out, err
	}

	var (
		usage mediadomain.Usage
		err   error
	)

	statusKey := "CASE WHEN m.deleted_at IS NOT NULL THEN '" + mediadomain.StatusDeleted + "' ELSE m.status END"
	if usage.ByStatus, err = group(statusKey); err != nil {
		return usage, err
	}

	if usage.ByContentType, err = group("m.content_type"); err != nil {
		return usage, err
	}

	for _, row := range usage.ByStatus {
		usage.Total.Count += row.Count
		usage.Total.Bytes += row.Bytes
	}

	var uploaders []uploaderRow

	err = db.Raw(`SELECT u.uuid AS user_uuid, u.username, count(*) AS count, coalesce(sum(m.size_bytes), 0) AS bytes
		FROM media_assets m LEFT JOIN users u ON u.id = m.uploaded_by
		WHERE `+scope+` GROUP BY u.uuid, u.username ORDER BY 4 DESC, 3 DESC, 1 LIMIT ?`,
		append(args, filter.TopLimit)...).Scan(&uploaders).Error
	if err != nil {
		return usage, err
	}

	usage.TopUploaders = make([]mediadomain.UploaderUsage, 0, len(uploaders))
	for _, u := range uploaders {
		entry := mediadomain.UploaderUsage{UserUUID: u.UserUUID, Count: u.Count, Bytes: u.Bytes}
		if u.Username != nil {
			entry.Username = *u.Username
		}

		usage.TopUploaders = append(usage.TopUploaders, entry)
	}

	return usage, nil
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
