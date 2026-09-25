package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seoDefaults() SEODefaults {
	return SEODefaults{
		SiteName:      "Blog",
		TitleTemplate: "{title} | {site}",
		Description:   "Site description",
		CanonicalBase: "https://blog.example.com",
		ShareImageURL: "https://cdn.example.com/share.png",
		TwitterCard:   TwitterSummaryLarge,
		AllowedHosts:  []string{"partner.example.org"},
	}
}

func codes(violations []FieldViolation) map[string]string {
	out := map[string]string{}
	for _, v := range violations {
		out[v.Field] = v.Code
	}

	return out
}

func TestSEOApplyNormalisesAndClears(t *testing.T) {
	t.Parallel()

	image := uuid.New()
	current := SEO{Title: "Old", OGImageUUID: &image, RobotsNoindex: true}

	title := "  Hello \n\t world  "
	empty := ""
	keywords := []string{" Go ", "go", "", "Rust  lang"}
	noindex := false

	next := current.Apply(SEOPatch{
		Title:         &title,
		Description:   &empty,
		Keywords:      &keywords,
		OGImage:       OptionalUUID{Present: true},
		RobotsNoindex: &noindex,
	})

	assert.Equal(t, "Hello world", next.Title)
	assert.Empty(t, next.Description)
	assert.Equal(t, []string{"Go", "Rust lang"}, next.Keywords)
	assert.Nil(t, next.OGImageUUID)
	assert.False(t, next.RobotsNoindex)
	assert.Equal(t, "Old", current.Title, "Apply must not mutate the receiver")
}

func TestSEOValidateReportsEveryField(t *testing.T) {
	t.Parallel()

	seo := SEO{
		Title:          "<script>alert(1)</script>",
		Description:    strings.Repeat("a", MaxSEODescription+1),
		Keywords:       make([]string, MaxSEOKeywords+1),
		TwitterCard:    "gallery",
		TwitterCreator: "no-at-sign",
		CanonicalURL:   "https://evil.example.net/post",
		OGURL:          "javascript:alert(1)",
	}

	got := codes(seo.Validate(seoDefaults()))

	assert.Equal(t, map[string]string{
		"seo_title":       SEOCodeMarkup,
		"seo_description": SEOCodeTooLong,
		"seo_keywords":    SEOCodeTooMany,
		"twitter_card":    SEOCodeInvalidFormat,
		"twitter_creator": SEOCodeInvalidFormat,
		"canonical_url":   SEOCodeHostNotAllowed,
		"og_url":          SEOCodeInvalidFormat,
	}, got)
}

func TestSEOValidateAcceptsSiteAndAllowedHosts(t *testing.T) {
	t.Parallel()

	seo := SEO{
		Title:          "Fine title",
		TwitterCreator: "@blog_team",
		CanonicalURL:   "https://BLOG.example.com/posts/x",
		OGURL:          "https://partner.example.org/x",
	}

	assert.Empty(t, seo.Validate(seoDefaults()))

	noBase := seoDefaults()
	noBase.CanonicalBase = ""
	seo.CanonicalURL = "https://anything.example.net/x"
	assert.Empty(t, seo.Validate(noBase), "without a canonical base any host is allowed")
}

func TestSEOWarningsAreAdvisory(t *testing.T) {
	t.Parallel()

	seo := SEO{Title: strings.Repeat("t", RecommendedSEOTitle+1), Description: strings.Repeat("d", RecommendedSEODesc+1)}

	assert.Empty(t, seo.Validate(seoDefaults()))
	assert.Equal(t, map[string]string{
		"seo_title":       SEOCodeTooLongAdvisory,
		"seo_description": SEOCodeTooLongAdvisory,
	}, codes(seo.Warnings()))
}

func TestCheckSEOSlug(t *testing.T) {
	t.Parallel()

	assert.Nil(t, CheckSEOSlug("hello-world-2"))
	require.NotNil(t, CheckSEOSlug("Hello World"))
	assert.Equal(t, SEOCodeInvalidFormat, CheckSEOSlug("double--hyphen").Code)
	assert.Equal(t, SEOCodeInvalidFormat, CheckSEOSlug("").Code)
	assert.Equal(t, SEOCodeSlugReserved, CheckSEOSlug("admin").Code)
}

func TestRenderSEOFallsBackToPostAndSite(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	post := Post{
		Title:       "My **bold** post",
		Slug:        "my-post",
		Content:     "# Heading\n\nSome <b>content</b> with a [link](https://x.test).",
		PublishedAt: &published,
		UpdatedAt:   published.Add(time.Hour),
	}

	meta := RenderSEO(SEOInput{Post: post, Defaults: seoDefaults(), Tags: []string{"go"}})

	assert.Equal(t, "My bold post | Blog", meta.Title)
	assert.Equal(t, "Heading Some content with a link.", meta.MetaDescription)
	assert.Equal(t, "https://blog.example.com/posts/my-post", meta.Canonical)
	assert.Equal(t, "index,follow", meta.Robots)
	assert.Equal(t, "My bold post", meta.OpenGraph["og:title"])
	assert.Equal(t, "https://cdn.example.com/share.png", meta.OpenGraph["og:image"])
	assert.Equal(t, "https://blog.example.com/posts/my-post", meta.OpenGraph["og:url"])
	assert.Equal(t, "2026-09-01T08:00:00Z", meta.OpenGraph["article:published_time"])
	assert.Equal(t, []string{"go"}, meta.OpenGraph["article:tag"])
	assert.Equal(t, "summary_large_image", meta.Twitter["twitter:card"])
	assert.Equal(t, "https://cdn.example.com/share.png", meta.Twitter["twitter:image"])
}

func TestRenderSEOPrefersExplicitValues(t *testing.T) {
	t.Parallel()

	meta := RenderSEO(SEOInput{
		Post: Post{Title: "Title", Slug: "t", Excerpt: "Excerpt"},
		SEO: SEO{
			Title: "SEO title", OGTitle: "OG title", TwitterTitle: "Tw title", TwitterCard: TwitterSummary,
			CanonicalURL: "https://blog.example.com/canonical", RobotsNoindex: true, Keywords: []string{"a", "b"},
		},
		Defaults:      seoDefaults(),
		OGImageURL:    "https://img/og.jpg",
		CoverImageURL: "https://img/cover.jpg",
	})

	assert.Equal(t, "SEO title", meta.Title)
	assert.Equal(t, "Excerpt", meta.MetaDescription)
	assert.Equal(t, "a, b", meta.MetaKeywords)
	assert.Equal(t, "https://blog.example.com/canonical", meta.Canonical)
	assert.Equal(t, "noindex,follow", meta.Robots)
	assert.Equal(t, "OG title", meta.OpenGraph["og:title"])
	assert.Equal(t, "https://img/og.jpg", meta.OpenGraph["og:image"])
	assert.Equal(t, "Tw title", meta.Twitter["twitter:title"])
	assert.Equal(t, "summary", meta.Twitter["twitter:card"])
	assert.Equal(t, "https://img/og.jpg", meta.Twitter["twitter:image"])
}

func TestSummaryTruncatesLongContent(t *testing.T) {
	t.Parallel()

	meta := RenderSEO(SEOInput{Post: Post{Title: "T", Content: strings.Repeat("word ", 100)}})

	assert.LessOrEqual(t, len([]rune(meta.MetaDescription)), 161)
	assert.True(t, strings.HasSuffix(meta.MetaDescription, "…"))
}

func TestSEOCodecAndFieldChanges(t *testing.T) {
	t.Parallel()

	image := uuid.New()
	seo := SEO{Title: "T", OGImageUUID: &image, RobotsNofollow: true}

	decoded, err := DecodeSEO(EncodeSEO(seo))
	require.NoError(t, err)
	assert.Equal(t, seo, decoded)
	assert.JSONEq(t, `{}`, string(EncodeSEO(SEO{})))

	assert.Equal(t, []string{"og_image_id", "seo_description", "seo_title"},
		SEOFieldChanges(seo, SEO{Title: "U", Description: "D", RobotsNofollow: true}))
}

func TestDiffSnapshotsDiffsSEOPerField(t *testing.T) {
	t.Parallel()

	prev := Snapshot{SEO: EncodeSEO(SEO{Title: "Old", RobotsNoindex: true})}
	next := Snapshot{SEO: EncodeSEO(SEO{Title: "New"})}

	fields, diff := DiffSnapshots(prev, next)

	assert.Equal(t, []string{FieldSEO}, fields)
	assert.Equal(t, map[string]any{
		"seo_title":      map[string]any{"from": "Old", "to": "New"},
		"robots_noindex": map[string]any{"from": true, "to": nil},
	}, diff[FieldSEO])
}
