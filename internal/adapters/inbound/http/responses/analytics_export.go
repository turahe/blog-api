package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

// AnalyticsExport renders a rollup export. status is pending, running, completed, failed, or
// expired (completed, archive deleted). The download fields are present only while the archive
// exists; failures carry no detail, which stays in the server log.
func AnalyticsExport(view analyticsservice.ExportView, now time.Time) gin.H {
	out := gin.H{
		"id":          view.UUID.String(),
		"status":      view.State(now),
		"grain":       string(view.Grain),
		"from":        view.FirstDay,
		"to":          view.LastDay,
		"timezone":    view.Timezone,
		"requestedAt": view.CreatedAt.UTC().Format(time.RFC3339),
		"completedAt": RFC3339(view.CompletedAt),
	}

	if view.DownloadURL != "" {
		out["downloadUrl"] = view.DownloadURL
		out["downloadExpiresAt"] = RFC3339(view.DownloadExpiresAt)
		out["archiveExpiresAt"] = RFC3339(view.ExpiresAt)
		out["sizeBytes"] = view.SizeBytes
	}

	return out
}

// AnalyticsExports renders a list of exports.
func AnalyticsExports(views []analyticsservice.ExportView, now time.Time) []gin.H {
	out := make([]gin.H, 0, len(views))
	for _, view := range views {
		out = append(out, AnalyticsExport(view, now))
	}

	return out
}
