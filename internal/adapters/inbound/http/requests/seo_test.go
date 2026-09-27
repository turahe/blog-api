package requests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

func TestNullableUUIDUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    NullableUUID
		wantErr bool
	}{
		{name: "absent leaves unchanged", body: `{}`, want: NullableUUID{}},
		{name: "null clears", body: `{"id": null}`, want: NullableUUID{Present: true}},
		{name: "value sets", body: `{"id": "` + idA.String() + `"}`, want: NullableUUID{Present: true, Value: &idA}},
		{name: "invalid uuid", body: `{"id": "nope"}`, want: NullableUUID{Present: true}, wantErr: true},
		{name: "wrong type", body: `{"id": 7}`, want: NullableUUID{Present: true}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dst struct {
				ID NullableUUID `json:"id"`
			}

			err := json.Unmarshal([]byte(tt.body), &dst)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, dst.ID)
		})
	}
}

func TestNullableUUIDUnmarshalJSONTrimsNull(t *testing.T) {
	t.Parallel()

	n := NullableUUID{Value: &idA}
	require.NoError(t, n.UnmarshalJSON([]byte(" null ")))
	assert.Equal(t, NullableUUID{Present: true}, n)
}

func TestUpdatePostSEOPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want postdomain.SEOPatch
	}{
		{name: "empty body changes nothing", body: `{}`, want: postdomain.SEOPatch{}},
		{
			name: "every field",
			body: `{
				"seoTitle": "T", "seoDescription": "D", "seoKeywords": ["a","b"], "slug": "s",
				"ogTitle": "OT", "ogDescription": "OD", "ogImageId": "` + idA.String() + `", "ogUrl": "https://o.test",
				"twitterCard": "summary_large_image", "twitterTitle": "TT", "twitterDescription": "TD",
				"twitterImageId": "` + idB.String() + `", "twitterCreator": "@me", "canonicalUrl": "https://c.test",
				"robotsNoindex": true, "robotsNofollow": false
			}`,
			want: postdomain.SEOPatch{
				Title: new("T"), Description: new("D"), Keywords: &[]string{"a", "b"}, Slug: new("s"),
				OGTitle: new("OT"), OGDescription: new("OD"),
				OGImage: postdomain.OptionalUUID{Present: true, Value: &idA}, OGURL: new("https://o.test"),
				TwitterCard: new(postdomain.TwitterSummaryLarge), TwitterTitle: new("TT"), TwitterDescription: new("TD"),
				TwitterImage:   postdomain.OptionalUUID{Present: true, Value: &idB},
				TwitterCreator: new("@me"), CanonicalURL: new("https://c.test"),
				RobotsNoindex: new(true), RobotsNofollow: new(false),
			},
		},
		{
			name: "empty strings and nulls clear",
			body: `{"seoTitle": "", "ogImageId": null, "twitterImageId": null, "twitterCard": ""}`,
			want: postdomain.SEOPatch{
				Title:        new(""),
				OGImage:      postdomain.OptionalUUID{Present: true},
				TwitterImage: postdomain.OptionalUUID{Present: true},
				TwitterCard:  new(postdomain.TwitterCard("")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var req UpdatePostSEO
			require.NoError(t, json.Unmarshal([]byte(tt.body), &req))

			assert.Equal(t, tt.want, req.Patch())
		})
	}
}

func TestPreviewPostSEOValidation(t *testing.T) {
	t.Parallel()

	runBindCases[PreviewPostSEO](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "title and excerpt valid", body: with(nil, "title", long(255), "excerpt", long(2000), "seoTitle", "T")},
		{name: "title too long", body: with(nil, "title", long(256)), want: errs("title", msgMaxChars("title", 255))},
		{name: "excerpt too long", body: with(nil, "excerpt", long(2001)), want: errs("excerpt", msgMaxChars("excerpt", 2000))},
		{
			name: "invalid image id is a form error",
			body: `{"ogImageId": "nope"}`,
			want: errs("_form", "The request body is invalid."),
		},
	})
}

func TestPreviewPostSEOEmbedsUpdate(t *testing.T) {
	t.Parallel()

	var req PreviewPostSEO
	require.NoError(t, json.Unmarshal([]byte(`{"title": "Draft", "excerpt": "E", "seoTitle": "S"}`), &req))

	assert.Equal(t, new("Draft"), req.Title)
	assert.Equal(t, new("E"), req.Excerpt)
	assert.Equal(t, postdomain.SEOPatch{Title: new("S")}, req.Patch())
}
