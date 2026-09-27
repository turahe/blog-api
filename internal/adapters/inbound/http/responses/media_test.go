package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

var (
	mediaID       = uuid.MustParse("0198a1b2-0000-7000-8000-0000000d0001")
	mediaUploader = uuid.MustParse("0198a1b2-0000-7000-8000-0000000d0002")
)

// pendingAsset is an asset just presigned: no dimensions, checksum, uploader, or tags yet.
func pendingAsset() mediadomain.MediaAsset {
	return mediadomain.MediaAsset{
		ID:               3,
		UUID:             mediaID,
		StorageKey:       "media/2026/08/cover.png",
		OriginalFilename: "cover.png",
		ContentType:      "image/png",
		SizeBytes:        1024,
		Disk:             "s3",
		Status:           "pending",
		CreatedAt:        time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:        time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}
}

const pendingAssetJSON = `"id": "0198a1b2-0000-7000-8000-0000000d0001", "storageKey": "media/2026/08/cover.png",
	"originalFilename": "cover.png", "contentType": "image/png", "sizeBytes": 1024,
	"width": null, "height": null, "checksumSha256": null, "disk": "s3", "status": "pending",
	"tags": [], "uploadedBy": null, "createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T10:00:00Z"`

func TestMediaPresign(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 8, 1, 10, 15, 0, 0, time.UTC)

	tests := []struct {
		name   string
		result mediadomain.PresignResult
		want   string
	}{
		{
			name:   "no required headers",
			result: mediadomain.PresignResult{Asset: pendingAsset(), UploadURL: "https://s3.example/put", ExpiresAt: expiresAt},
			want: `{"mediaId": "0198a1b2-0000-7000-8000-0000000d0001", "storageKey": "media/2026/08/cover.png",
				"uploadUrl": "https://s3.example/put", "requiredHeaders": {}, "expiresAt": "2026-08-01T10:15:00Z", "disk": "s3"}`,
		},
		{
			name: "required headers",
			result: mediadomain.PresignResult{
				Asset:           pendingAsset(),
				UploadURL:       "https://s3.example/put",
				RequiredHeaders: map[string]string{"Content-Type": "image/png"},
				ExpiresAt:       expiresAt,
			},
			want: `{"mediaId": "0198a1b2-0000-7000-8000-0000000d0001", "storageKey": "media/2026/08/cover.png",
				"uploadUrl": "https://s3.example/put", "requiredHeaders": {"Content-Type": "image/png"},
				"expiresAt": "2026-08-01T10:15:00Z", "disk": "s3"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, MediaPresign(tt.result))
		})
	}
}

func TestMediaAssetWithVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variants map[string]string
		want     string
	}{
		{name: "transforms not configured", variants: nil, want: `{` + pendingAssetJSON + `}`},
		{name: "asset cannot be transformed", variants: map[string]string{}, want: `{` + pendingAssetJSON + `, "variants": {}}`},
		{
			name:     "preset URLs",
			variants: map[string]string{"thumb": "https://cdn.example/thumb.webp"},
			want:     `{` + pendingAssetJSON + `, "variants": {"thumb": "https://cdn.example/thumb.webp"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, MediaAssetWithVariants(pendingAsset(), tt.variants))
		})
	}
}

func TestMediaUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage mediadomain.Usage
		want  string
	}{
		{
			name:  "empty library",
			usage: mediadomain.Usage{},
			want:  `{"total": {"count": 0, "bytes": 0}, "byStatus": [], "byContentType": [], "topUploaders": []}`,
		},
		{
			name: "breakdowns and uploaders",
			usage: mediadomain.Usage{
				Total:         mediadomain.UsageRow{Count: 3, Bytes: 3072},
				ByStatus:      []mediadomain.UsageRow{{Key: "ready", Count: 3, Bytes: 3072}},
				ByContentType: []mediadomain.UsageRow{{Key: "image/png", Count: 2, Bytes: 2048}, {Key: "image/jpeg", Count: 1, Bytes: 1024}},
				TopUploaders: []mediadomain.UploaderUsage{
					{UserUUID: &mediaUploader, Username: "gopher", Count: 2, Bytes: 2048},
					{Username: "ignored without an account", Count: 1, Bytes: 1024},
				},
			},
			want: `{"total": {"count": 3, "bytes": 3072},
				"byStatus": [{"status": "ready", "count": 3, "bytes": 3072}],
				"byContentType": [
					{"contentType": "image/png", "count": 2, "bytes": 2048},
					{"contentType": "image/jpeg", "count": 1, "bytes": 1024}
				],
				"topUploaders": [
					{"userId": "0198a1b2-0000-7000-8000-0000000d0002", "username": "gopher", "count": 2, "bytes": 2048},
					{"userId": null, "username": null, "count": 1, "bytes": 1024}
				]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, MediaUsage(tt.usage))
		})
	}
}

func TestMediaAsset(t *testing.T) {
	t.Parallel()

	ready := pendingAsset()
	ready.Status = "ready"
	ready.Width, ready.Height = new(1200), new(630)
	ready.ChecksumSHA256 = new("abc123")
	ready.UploadedByUUID = &mediaUploader
	ready.Tags = []string{"cover", "hero"}

	tests := []struct {
		name  string
		asset mediadomain.MediaAsset
		want  string
	}{
		{name: "pending upload", asset: pendingAsset(), want: `{` + pendingAssetJSON + `}`},
		{
			name:  "ready asset",
			asset: ready,
			want: `{"id": "0198a1b2-0000-7000-8000-0000000d0001", "storageKey": "media/2026/08/cover.png",
				"originalFilename": "cover.png", "contentType": "image/png", "sizeBytes": 1024,
				"width": 1200, "height": 630, "checksumSha256": "abc123", "disk": "s3", "status": "ready",
				"tags": ["cover", "hero"], "uploadedBy": "0198a1b2-0000-7000-8000-0000000d0002",
				"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, MediaAsset(tt.asset))
		})
	}
}
