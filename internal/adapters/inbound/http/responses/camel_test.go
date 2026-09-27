package responses

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

func TestCamelKey(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"title":                "title",
		"post_id":              "postId",
		"cover_image_media_id": "coverImageMediaId",
		"checksum_sha256":      "checksumSha256",
		"trailing_":            "trailing",
	} {
		assert.Equal(t, want, CamelKey(in), in)
	}
}

func TestCamelTree(t *testing.T) {
	t.Parallel()

	unencodable := make(chan int)

	tests := []struct {
		name string
		in   any
		want any
	}{
		{
			name: "renames keys at every depth",
			in: map[string]any{
				"post_id": 1,
				"media_items": []any{
					map[string]any{"sort_order": 2, "tags": []string{"snake_case_value"}},
				},
			},
			want: map[string]any{
				"postId": float64(1),
				"mediaItems": []any{
					map[string]any{"sortOrder": float64(2), "tags": []any{"snake_case_value"}},
				},
			},
		},
		{
			name: "scalars pass through",
			in:   "snake_case",
			want: "snake_case",
		},
		{
			name: "unencodable values are returned unchanged",
			in:   unencodable,
			want: unencodable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, camelTree(tt.in))
		})
	}
}

func TestFieldViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		violations []postdomain.FieldViolation
		want       string
	}{
		{name: "none", violations: nil, want: `[]`},
		{
			name: "field named as in requests",
			violations: []postdomain.FieldViolation{
				{Field: "seo_title", Code: "too_long", Message: "The SEO title may not be greater than 70 characters."},
				{Field: "slug", Code: "taken", Message: "The slug has already been taken."},
			},
			want: `[
				{"field": "seoTitle", "code": "too_long", "message": "The SEO title may not be greater than 70 characters."},
				{"field": "slug", "code": "taken", "message": "The slug has already been taken."}
			]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, FieldViolations(tt.violations))
		})
	}
}

func TestPostRevisionRenamesStoredFieldNames(t *testing.T) {
	t.Parallel()

	asset := uuid.New()
	rev := postdomain.Revision{
		ChangedFields: []string{postdomain.FieldCommentPolicy, postdomain.FieldContent, postdomain.FieldMedia, postdomain.FieldSEO},
		Diff: map[string]any{
			postdomain.FieldCommentPolicy: map[string]any{"from": "open", "to": "read_only"},
			postdomain.FieldContent:       map[string]any{"from_length": 3, "to_length": 5},
			postdomain.FieldMedia: map[string]any{
				"added":   []postdomain.RevisionMedia{{MediaAssetID: asset, Kind: "cover", SortOrder: 1}},
				"removed": []postdomain.RevisionMedia{},
			},
			postdomain.FieldSEO: map[string]any{"seo_title": map[string]any{"from": nil, "to": "Hi"}},
		},
		Snapshot: postdomain.Snapshot{
			Media: []postdomain.RevisionMedia{{MediaAssetID: asset, Kind: "cover", SortOrder: 1}},
			SEO:   json.RawMessage(`{"seo_title":"Hi","og_image_id":null}`),
		},
	}

	out := PostRevision(rev, true, true)

	assert.Equal(t, []string{"commentPolicy", "content", "media", "seo"}, out["changedFields"])

	diff, ok := out["diff"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"from": "open", "to": "read_only"}, diff["commentPolicy"], "values keep their case")
	assert.Equal(t, map[string]any{"fromLength": float64(3), "toLength": float64(5)}, diff["content"])
	assert.Equal(t, map[string]any{"seoTitle": map[string]any{"from": nil, "to": "Hi"}}, diff["seo"])

	media, ok := diff["media"].(map[string]any)
	require.True(t, ok)
	added, ok := media["added"].([]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"mediaAssetId": asset.String(), "kind": "cover", "sortOrder": float64(1)}, added[0])

	snapshot, ok := out["snapshot"].(gin.H)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"seoTitle": "Hi", "ogImageId": nil}, snapshot["seo"])
	assert.Equal(t, []gin.H{{"mediaAssetId": asset, "kind": "cover", "sortOrder": 1}}, snapshot["media"])
}

func TestPostRevisionWithoutDiffOrSEO(t *testing.T) {
	t.Parallel()

	out := PostRevision(postdomain.Revision{}, true, true)

	assert.Equal(t, []string{}, out["changedFields"])
	assert.Equal(t, map[string]any{}, out["diff"])
	snapshot, ok := out["snapshot"].(gin.H)
	require.True(t, ok)
	assert.Equal(t, map[string]any{}, snapshot["seo"])
}

func TestNotificationRenamesPayloadKeys(t *testing.T) {
	t.Parallel()

	out := Notification(notificationdomain.Notification{Payload: map[string]string{"post_id": "p", "url": "/x"}})
	assert.Equal(t, map[string]string{"postId": "p", "url": "/x"}, out["data"])

	empty := Notification(notificationdomain.Notification{})
	assert.Equal(t, map[string]string{}, empty["data"])
}
