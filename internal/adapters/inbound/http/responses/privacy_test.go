package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
)

func TestPrivacyRequest(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	completedAt := time.Date(2026, 8, 1, 10, 5, 0, 0, time.UTC)
	archiveExpiresAt := time.Date(2026, 8, 8, 10, 5, 0, 0, time.UTC)

	pending := privacydomain.Request{
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-000000002001"),
		Kind:      privacydomain.KindErase,
		Status:    privacydomain.StatusPending,
		CreatedAt: requestedAt,
	}

	export := pending
	export.Kind = privacydomain.KindExport
	export.Status = privacydomain.StatusCompleted
	export.StorageKey = new("privacy/2001.zip")
	export.ExpiresAt = &archiveExpiresAt
	export.CompletedAt = &completedAt

	tests := []struct {
		name              string
		request           privacydomain.Request
		downloadURL       string
		downloadExpiresAt *time.Time
		want              string
	}{
		{
			name:    "pending erasure",
			request: pending,
			want: `{"id": "0198a1b2-0000-7000-8000-000000002001", "kind": "erase", "status": "pending",
				"requestedAt": "2026-08-01T10:00:00Z", "completedAt": null}`,
		},
		{
			name:              "completed export with a download",
			request:           export,
			downloadURL:       "https://cdn.example/2001.zip?sig=x",
			downloadExpiresAt: new(completedAt.Add(15 * time.Minute)),
			want: `{"id": "0198a1b2-0000-7000-8000-000000002001", "kind": "export", "status": "completed",
				"requestedAt": "2026-08-01T10:00:00Z", "completedAt": "2026-08-01T10:05:00Z",
				"downloadUrl": "https://cdn.example/2001.zip?sig=x", "downloadExpiresAt": "2026-08-01T10:20:00Z",
				"archiveExpiresAt": "2026-08-08T10:05:00Z"}`,
		},
		{
			name:    "completed export whose archive is gone",
			request: export,
			want: `{"id": "0198a1b2-0000-7000-8000-000000002001", "kind": "export", "status": "completed",
				"requestedAt": "2026-08-01T10:00:00Z", "completedAt": "2026-08-01T10:05:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, PrivacyRequest(tt.request, tt.downloadURL, tt.downloadExpiresAt))
		})
	}
}
