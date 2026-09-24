package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

// MediaPresign serializes a media upload presign result.
func MediaPresign(result mediadomain.PresignResult) gin.H {
	headers := result.RequiredHeaders
	if headers == nil {
		headers = map[string]string{}
	}
	return gin.H{
		"media_id":         result.Asset.ID.String(),
		"storage_key":      result.Asset.StorageKey,
		"upload_url":       result.UploadURL,
		"required_headers": headers,
		"expires_at":       result.ExpiresAt.UTC().Format(time.RFC3339),
		"disk":             result.Asset.Disk,
	}
}

// MediaAsset serializes a media asset resource.
func MediaAsset(asset mediadomain.MediaAsset) gin.H {
	var uploadedBy any
	if asset.UploadedBy != nil {
		uploadedBy = asset.UploadedBy.String()
	}
	tags := asset.Tags
	if tags == nil {
		tags = []string{}
	}
	return gin.H{
		"id":                asset.ID.String(),
		"storage_key":       asset.StorageKey,
		"original_filename": asset.OriginalFilename,
		"content_type":      asset.ContentType,
		"size_bytes":        asset.SizeBytes,
		"width":             asset.Width,
		"height":            asset.Height,
		"checksum_sha256":   asset.ChecksumSHA256,
		"disk":              asset.Disk,
		"status":            asset.Status,
		"tags":              tags,
		"uploaded_by":       uploadedBy,
		"created_at":        asset.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":        asset.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
