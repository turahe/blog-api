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
		"mediaId":         result.Asset.UUID.String(),
		"storageKey":      result.Asset.StorageKey,
		"uploadUrl":       result.UploadURL,
		"requiredHeaders": headers,
		"expiresAt":       result.ExpiresAt.UTC().Format(time.RFC3339),
		"disk":            result.Asset.Disk,
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

		uploaders = append(uploaders, gin.H{"userId": id, "username": username, "count": up.Count, "bytes": up.Bytes})
	}

	return gin.H{
		"total":         gin.H{"count": u.Total.Count, "bytes": u.Total.Bytes},
		"byStatus":      rows(u.ByStatus, "status"),
		"byContentType": rows(u.ByContentType, "contentType"),
		"topUploaders":  uploaders,
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
		"id":               asset.UUID.String(),
		"storageKey":       asset.StorageKey,
		"originalFilename": asset.OriginalFilename,
		"contentType":      asset.ContentType,
		"sizeBytes":        asset.SizeBytes,
		"width":            asset.Width,
		"height":           asset.Height,
		"checksumSha256":   asset.ChecksumSHA256,
		"disk":             asset.Disk,
		"status":           asset.Status,
		"tags":             tags,
		"uploadedBy":       uploadedBy,
		"createdAt":        asset.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt":        asset.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
