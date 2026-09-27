package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

var exportNow = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

func sampleExport(status analyticsdomain.ExportStatus) analyticsservice.ExportView {
	return analyticsservice.ExportView{Export: analyticsdomain.Export{
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-00000000e001"),
		Grain:     analyticsdomain.GrainWeek,
		FirstDay:  "2026-07-06",
		LastDay:   "2026-08-02",
		Timezone:  "Asia/Jakarta",
		Status:    status,
		CreatedAt: time.Date(2026, 8, 10, 18, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
	}}
}

const exportBaseJSON = `"id": "0198a1b2-0000-7000-8000-00000000e001", "grain": "week",
	"from": "2026-07-06", "to": "2026-08-02", "timezone": "Asia/Jakarta", "requestedAt": "2026-08-10T11:00:00Z"`

func TestAnalyticsExport(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 8, 10, 11, 5, 0, 0, time.UTC)

	ready := sampleExport(analyticsdomain.ExportCompleted)
	ready.StorageKey = new("exports/e001.zip")
	ready.SizeBytes = new(int64(2048))
	ready.ExpiresAt = new(exportNow.Add(24 * time.Hour))
	ready.CompletedAt = &completedAt
	ready.DownloadURL = "https://cdn.example/e001.zip?sig=x"
	ready.DownloadExpiresAt = new(exportNow.Add(15 * time.Minute))

	expired := sampleExport(analyticsdomain.ExportCompleted)
	expired.CompletedAt = &completedAt

	tests := []struct {
		name string
		view analyticsservice.ExportView
		want string
	}{
		{
			name: "pending",
			view: sampleExport(analyticsdomain.ExportPending),
			want: `{` + exportBaseJSON + `, "status": "pending", "completedAt": null}`,
		},
		{
			name: "completed with a download",
			view: ready,
			want: `{` + exportBaseJSON + `, "status": "completed", "completedAt": "2026-08-10T11:05:00Z",
				"downloadUrl": "https://cdn.example/e001.zip?sig=x", "downloadExpiresAt": "2026-08-10T12:15:00Z",
				"archiveExpiresAt": "2026-08-11T12:00:00Z", "sizeBytes": 2048}`,
		},
		{
			name: "completed without an archive is expired",
			view: expired,
			want: `{` + exportBaseJSON + `, "status": "expired", "completedAt": "2026-08-10T11:05:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsExport(tt.view, exportNow))
		})
	}
}

func TestAnalyticsExports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		views []analyticsservice.ExportView
		want  string
	}{
		{name: "none", views: nil, want: `[]`},
		{
			name:  "each export in order",
			views: []analyticsservice.ExportView{sampleExport(analyticsdomain.ExportRunning), sampleExport(analyticsdomain.ExportFailed)},
			want: `[
				{` + exportBaseJSON + `, "status": "running", "completedAt": null},
				{` + exportBaseJSON + `, "status": "failed", "completedAt": null}
			]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsExports(tt.views, exportNow))
		})
	}
}
