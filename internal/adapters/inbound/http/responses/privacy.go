package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
)

// PrivacyRequest renders an export or erasure request. The download fields are present only
// for a completed export whose archive still exists.
func PrivacyRequest(request privacydomain.Request, downloadURL string, downloadExpiresAt *time.Time) gin.H {
	out := gin.H{
		"id":           request.UUID.String(),
		"kind":         string(request.Kind),
		"status":       string(request.Status),
		"requested_at": request.CreatedAt.UTC().Format(time.RFC3339),
		"completed_at": RFC3339(request.CompletedAt),
	}

	if downloadURL != "" {
		out["download_url"] = downloadURL
		out["download_expires_at"] = RFC3339(downloadExpiresAt)
		out["archive_expires_at"] = RFC3339(request.ExpiresAt)
	}

	return out
}
