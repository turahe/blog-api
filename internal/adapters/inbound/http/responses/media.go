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
		"media_id":         result.Asset.UUID.String(),
		"storage_key":      result.Asset.StorageKey,
		"upload_url":       result.UploadURL,
		"required_headers": headers,
		"expires_at":       result.ExpiresAt.UTC().Format(time.RFC3339),
		"disk":             result.Asset.Disk,
	}
}

// MediaAssetWithVariants serializes an asset with its preset URLs. A nil map (transforms not
// configured) omits the variants field; assets that cannot be transformed get an empty map.
func MediaAssetWithVariants(asset mediadomain.MediaAsset, variants map[string]string) gin.H {
	out := MediaAsset(asset)
	if variants != nil {
		out["variants"] = variants
	}

	return out
}

// MediaUsage serializes a storage usage report.
func MediaUsage(u mediadomain.Usage) gin.H {
	rows := func(in []mediadomain.UsageRow, key string) []gin.H {
		out := make([]gin.H, 0, len(in))
		for _, r := range in {
			out = append(out, gin.H{key: r.Key, "count": r.Count, "bytes": r.Bytes})
		}

		return out
	}

	uploaders := make([]gin.H, 0, len(u.TopUploaders))
	for _, up := range u.TopUploaders {
		var id, username any
		if up.UserUUID != nil {
			id, username = up.UserUUID.String(), up.Username
		}

		uploaders = append(uploaders, gin.H{"user_id": id, "username": username, "count": up.Count, "bytes": up.Bytes})
	}

	return gin.H{
		"total":           gin.H{"count": u.Total.Count, "bytes": u.Total.Bytes},
		"by_status":       rows(u.ByStatus, "status"),
		"by_content_type": rows(u.ByContentType, "content_type"),
		"top_uploaders":   uploaders,
	}
}

// MediaAsset serializes a media asset resource.
func MediaAsset(asset mediadomain.MediaAsset) gin.H {
	var uploadedBy any
	if asset.UploadedByUUID != nil {
		uploadedBy = asset.UploadedByUUID.String()
	}

	tags := asset.Tags
	if tags == nil {
		tags = []string{}
	}

	return gin.H{
		"id":                asset.UUID.String(),
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
