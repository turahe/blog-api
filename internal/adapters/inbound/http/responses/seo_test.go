package responses

import (
	"testing"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

func TestPostSEO(t *testing.T) {
	t.Parallel()

	ogImage := uuid.MustParse("0198a1b2-0000-7000-8000-000000006002")
	twitterImage := uuid.MustParse("0198a1b2-0000-7000-8000-000000006003")

	tests := []struct {
		name string
		view postservice.SEOView
		want string
	}{
		{
			name: "every field falls back",
			view: postservice.SEOView{PostUUID: postID, Slug: "hello"},
			want: `{"postId": "0198a1b2-0000-7000-8000-000000001001", "slug": "hello", "seoTitle": "",
				"seoDescription": "", "seoKeywords": [], "ogTitle": "", "ogDescription": "", "ogImageId": null,
				"ogUrl": "", "twitterCard": "", "twitterTitle": "", "twitterDescription": "", "twitterImageId": null,
				"twitterCreator": "", "canonicalUrl": "", "robotsNoindex": false, "robotsNofollow": false,
				"warnings": []}`,
		},
		{
			name: "every field set with warnings",
			view: postservice.SEOView{
				PostUUID: postID,
				Slug:     "hello",
				SEO: postdomain.SEO{
					Title: "Hello | Blog", Description: "About hello", Keywords: []string{"go", "hello"},
					OGTitle: "Hello!", OGDescription: "OG about", OGImageUUID: &ogImage, OGURL: "https://blog.example/hello",
					TwitterCard: postdomain.TwitterSummaryLarge, TwitterTitle: "Tw", TwitterDescription: "Tw about",
					TwitterImageUUID: &twitterImage, TwitterCreator: "@gopher", CanonicalURL: "https://blog.example/hello",
					RobotsNoindex: true, RobotsNofollow: true,
				},
				Warnings: []postdomain.FieldViolation{{Field: "seo_description", Code: "short", Message: "Too short."}},
			},
			want: `{"postId": "0198a1b2-0000-7000-8000-000000001001", "slug": "hello", "seoTitle": "Hello | Blog",
				"seoDescription": "About hello", "seoKeywords": ["go", "hello"], "ogTitle": "Hello!",
				"ogDescription": "OG about", "ogImageId": "0198a1b2-0000-7000-8000-000000006002",
				"ogUrl": "https://blog.example/hello", "twitterCard": "summary_large_image", "twitterTitle": "Tw",
				"twitterDescription": "Tw about", "twitterImageId": "0198a1b2-0000-7000-8000-000000006003",
				"twitterCreator": "@gopher", "canonicalUrl": "https://blog.example/hello",
				"robotsNoindex": true, "robotsNofollow": true,
				"warnings": [{"field": "seoDescription", "code": "short", "message": "Too short."}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, PostSEO(tt.view))
		})
	}
}

func TestSEOMeta(t *testing.T) {
	t.Parallel()

	base := postdomain.SEOMeta{
		Title:           "Hello | Blog",
		MetaDescription: "About hello",
		Robots:          "index,follow",
		OpenGraph:       map[string]any{"og:title": "Hello", "og:image:width": 1200},
		Twitter:         map[string]string{"twitter:card": "summary"},
	}

	const common = `"title": "Hello | Blog", "metaDescription": "About hello", "robots": "index,follow",
		"openGraph": {"og:title": "Hello", "og:image:width": 1200}, "twitter": {"twitter:card": "summary"}`

	full := base
	full.MetaKeywords, full.Canonical = "go,hello", "https://blog.example/hello"

	tests := []struct {
		name string
		meta postdomain.SEOMeta
		want string
	}{
		{name: "optional tags omitted", meta: base, want: `{` + common + `}`},
		{
			name: "keywords and canonical",
			meta: full,
			want: `{` + common + `, "metaKeywords": "go,hello", "canonical": "https://blog.example/hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, SEOMeta(tt.meta))
		})
	}
}

func TestSEOPreview(t *testing.T) {
	t.Parallel()

	preview := postdomain.SEOPreview{
		Search: postdomain.SearchPreview{Title: "Hello", URL: "https://blog.example/hello", Description: "About"},
		OG: postdomain.SocialPreview{
			Title: "OG", Description: "OG about", ImageURL: "https://cdn.example/og.png",
			URL: "https://blog.example/hello", SiteName: "Blog",
		},
		Twitter: postdomain.TwitterPreview{
			Card: "summary", Title: "Tw", Description: "Tw about", ImageURL: "https://cdn.example/tw.png", Creator: "@gopher",
		},
	}

	withWarning := preview
	withWarning.Warnings = []postdomain.FieldViolation{{Field: "og_image_id", Code: "missing", Message: "Add an image."}}

	const cards = `"searchPreview": {"title": "Hello", "url": "https://blog.example/hello", "description": "About"},
		"ogPreview": {"title": "OG", "description": "OG about", "imageUrl": "https://cdn.example/og.png",
			"url": "https://blog.example/hello", "siteName": "Blog"},
		"twitterPreview": {"card": "summary", "title": "Tw", "description": "Tw about",
			"imageUrl": "https://cdn.example/tw.png", "creator": "@gopher"}`

	tests := []struct {
		name    string
		preview postdomain.SEOPreview
		want    string
	}{
		{name: "no warnings", preview: preview, want: `{` + cards + `, "warnings": []}`},
		{
			name:    "warnings",
			preview: withWarning,
			want:    `{` + cards + `, "warnings": [{"field": "ogImageId", "code": "missing", "message": "Add an image."}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, SEOPreview(tt.preview))
		})
	}
}
