package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// SEO limits and soft-warning thresholds.
const (
	MaxSEOTitle          = 200
	MaxSEODescription    = 500
	MaxSEOKeywords       = 20
	MaxSEOKeyword        = 60
	MaxSEOURL            = 2048
	MaxSEOSlug           = 200
	RecommendedSEOTitle  = 60
	RecommendedSEODesc   = 160
	derivedDescriptionAt = 160
)

// ErrSlugEditForbidden is returned when a caller without post.slug.edit changes a slug.
var ErrSlugEditForbidden = errors.New("changing the slug requires post.slug.edit")

// TwitterCard is a Twitter (X) card type.
type TwitterCard string

// Twitter card types.
const (
	TwitterSummary      TwitterCard = "summary"
	TwitterSummaryLarge TwitterCard = "summary_large_image"
	TwitterApp          TwitterCard = "app"
	TwitterPlayer       TwitterCard = "player"
)

// SEO violation and warning codes.
const (
	SEOCodeTooLong         = "too_long"
	SEOCodeTooMany         = "too_many"
	SEOCodeInvalidFormat   = "invalid_format"
	SEOCodeMarkup          = "markup_not_allowed"
	SEOCodeHostNotAllowed  = "host_not_allowed"
	SEOCodeNotFound        = "not_found"
	SEOCodeNotImage        = "not_image"
	SEOCodeSlugTaken       = "slug_taken"
	SEOCodeSlugReserved    = "slug_reserved"
	SEOCodeTooLongAdvisory = "too_long_recommended"
)

// ReservedSlugs cannot be set through the SEO endpoint: they collide with site routes.
var ReservedSlugs = []string{
	"admin", "api", "assets", "auth", "categories", "docs", "feed", "health", "healthz", "login",
	"logout", "metrics", "register", "rss", "search", "sitemap", "static", "swagger", "tags",
}

var twitterCreatorPattern = regexp.MustCompile(`^@[A-Za-z0-9_]{1,15}$`)

// SEO is a post's search and social configuration. Empty fields fall back to values
// derived from the post and the site settings when rendered.
type SEO struct {
	Title              string      `json:"seo_title,omitempty"`
	Description        string      `json:"seo_description,omitempty"`
	Keywords           []string    `json:"seo_keywords,omitempty"`
	OGTitle            string      `json:"og_title,omitempty"`
	OGDescription      string      `json:"og_description,omitempty"`
	OGImageUUID        *uuid.UUID  `json:"og_image_id,omitempty"`
	OGURL              string      `json:"og_url,omitempty"`
	TwitterCard        TwitterCard `json:"twitter_card,omitempty"`
	TwitterTitle       string      `json:"twitter_title,omitempty"`
	TwitterDescription string      `json:"twitter_description,omitempty"`
	TwitterImageUUID   *uuid.UUID  `json:"twitter_image_id,omitempty"`
	TwitterCreator     string      `json:"twitter_creator,omitempty"`
	CanonicalURL       string      `json:"canonical_url,omitempty"`
	RobotsNoindex      bool        `json:"robots_noindex,omitempty"`
	RobotsNofollow     bool        `json:"robots_nofollow,omitempty"`
}

// EncodeSEO returns seo as a JSON object holding only the fields that are set.
func EncodeSEO(seo SEO) json.RawMessage {
	raw, err := json.Marshal(seo)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return raw
}

// DecodeSEO parses a revision's SEO snapshot; empty input is the zero SEO.
func DecodeSEO(raw json.RawMessage) (SEO, error) {
	var seo SEO
	if len(strings.TrimSpace(string(raw))) == 0 {
		return seo, nil
	}

	if err := json.Unmarshal(raw, &seo); err != nil {
		return SEO{}, fmt.Errorf("decode seo snapshot: %w", err)
	}

	return seo, nil
}

// OptionalUUID is a tri-state reference change: Present=false leaves it, Present=true
// sets Value (nil clears).
type OptionalUUID struct {
	Present bool
	Value   *uuid.UUID
}

// SEOPatch holds SEO changes; nil fields are left unchanged, and an empty string clears
// a text field.
type SEOPatch struct {
	Title              *string
	Description        *string
	Keywords           *[]string
	Slug               *string
	OGTitle            *string
	OGDescription      *string
	OGImage            OptionalUUID
	OGURL              *string
	TwitterCard        *TwitterCard
	TwitterTitle       *string
	TwitterDescription *string
	TwitterImage       OptionalUUID
	TwitterCreator     *string
	CanonicalURL       *string
	RobotsNoindex      *bool
	RobotsNofollow     *bool
}

// Apply returns seo with patch applied and text fields normalised: trimmed, internal
// whitespace collapsed, and keywords de-duplicated case-insensitively.
func (seo SEO) Apply(patch SEOPatch) SEO {
	text := func(dst, src *string) {
		if src != nil {
			*dst = collapseSpace(*src)
		}
	}

	text(&seo.Title, patch.Title)
	text(&seo.Description, patch.Description)
	text(&seo.OGTitle, patch.OGTitle)
	text(&seo.OGDescription, patch.OGDescription)
	text(&seo.OGURL, patch.OGURL)
	text(&seo.TwitterTitle, patch.TwitterTitle)
	text(&seo.TwitterDescription, patch.TwitterDescription)
	text(&seo.TwitterCreator, patch.TwitterCreator)
	text(&seo.CanonicalURL, patch.CanonicalURL)

	if patch.Keywords != nil {
		seo.Keywords = normaliseKeywords(*patch.Keywords)
	}

	if patch.TwitterCard != nil {
		seo.TwitterCard = *patch.TwitterCard
	}

	if patch.OGImage.Present {
		seo.OGImageUUID = patch.OGImage.Value
	}

	if patch.TwitterImage.Present {
		seo.TwitterImageUUID = patch.TwitterImage.Value
	}

	if patch.RobotsNoindex != nil {
		seo.RobotsNoindex = *patch.RobotsNoindex
	}

	if patch.RobotsNofollow != nil {
		seo.RobotsNofollow = *patch.RobotsNofollow
	}

	return seo
}

// FieldViolation is one field-level validation failure or advisory warning.
type FieldViolation struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SEOValidationError lists every invalid SEO field; it matches ErrValidation.
type SEOValidationError struct {
	Violations []FieldViolation
}

func (e *SEOValidationError) Error() string {
	return fmt.Sprintf("%d invalid seo field(s)", len(e.Violations))
}

// Unwrap makes SEOValidationError match ErrValidation.
func (e *SEOValidationError) Unwrap() error { return ErrValidation }

// SEODefaults are the site-level values SEO rendering falls back to.
type SEODefaults struct {
	SiteName      string
	TitleTemplate string
	Description   string
	// CanonicalBase is the site's canonical base URL; empty means canonical URLs cannot
	// be derived and any http(s) host is accepted.
	CanonicalBase string
	ShareImageURL string
	TwitterCard   TwitterCard
	// AllowedHosts are extra hosts canonical_url and og_url may use.
	AllowedHosts []string
}

// Validate returns seo's hard violations under defaults' host rules.
func (seo SEO) Validate(defaults SEODefaults) []FieldViolation {
	var out []FieldViolation

	add := func(field, code, message string) {
		out = append(out, FieldViolation{Field: field, Code: code, Message: message})
	}

	texts := []struct {
		field string
		value string
		max   int
	}{
		{"seo_title", seo.Title, MaxSEOTitle},
		{"seo_description", seo.Description, MaxSEODescription},
		{"og_title", seo.OGTitle, MaxSEOTitle},
		{"og_description", seo.OGDescription, MaxSEODescription},
		{"twitter_title", seo.TwitterTitle, MaxSEOTitle},
		{"twitter_description", seo.TwitterDescription, MaxSEODescription},
	}
	for _, t := range texts {
		if code, message := checkText(t.value, t.max); code != "" {
			add(t.field, code, message)
		}
	}

	if len(seo.Keywords) > MaxSEOKeywords {
		add("seo_keywords", SEOCodeTooMany, fmt.Sprintf("at most %d keywords allowed", MaxSEOKeywords))
	}

	for _, keyword := range seo.Keywords {
		if code, message := checkText(keyword, MaxSEOKeyword); code != "" {
			add("seo_keywords", code, "keyword "+message)
			break
		}
	}

	switch seo.TwitterCard {
	case "", TwitterSummary, TwitterSummaryLarge, TwitterApp, TwitterPlayer:
	default:
		add("twitter_card", SEOCodeInvalidFormat, "twitter_card must be summary, summary_large_image, app, or player")
	}

	if seo.TwitterCreator != "" && !twitterCreatorPattern.MatchString(seo.TwitterCreator) {
		add("twitter_creator", SEOCodeInvalidFormat, "twitter_creator must be @ followed by 1-15 letters, digits, or underscores")
	}

	for field, raw := range map[string]string{"canonical_url": seo.CanonicalURL, "og_url": seo.OGURL} {
		if code, message := checkSEOURL(raw, defaults); code != "" {
			add(field, code, message)
		}
	}

	slices.SortStableFunc(out, func(a, b FieldViolation) int { return strings.Compare(a.Field, b.Field) })

	return out
}

// Warnings returns advisory notes that do not block saving.
func (seo SEO) Warnings() []FieldViolation {
	out := []FieldViolation{}

	if n := utf8.RuneCountInString(seo.Title); n > RecommendedSEOTitle {
		out = append(out, FieldViolation{
			Field: "seo_title", Code: SEOCodeTooLongAdvisory,
			Message: fmt.Sprintf("SEO title is longer than the recommended %d characters and may be truncated in search results", RecommendedSEOTitle),
		})
	}

	if n := utf8.RuneCountInString(seo.Description); n > RecommendedSEODesc {
		out = append(out, FieldViolation{
			Field: "seo_description", Code: SEOCodeTooLongAdvisory,
			Message: fmt.Sprintf("SEO description is longer than the recommended %d characters and may be truncated in search results", RecommendedSEODesc),
		})
	}

	return out
}

// SEOFieldChanges returns the SEO JSON fields that differ between a and b, sorted.
func SEOFieldChanges(a, b SEO) []string {
	var before, after map[string]any

	_ = json.Unmarshal(EncodeSEO(a), &before)
	_ = json.Unmarshal(EncodeSEO(b), &after)

	return changedKeys(before, after)
}

// CheckSEOSlug validates the format of a slug set through the SEO endpoint and rejects
// reserved route names; uniqueness is checked against the repository separately.
func CheckSEOSlug(slug string) *FieldViolation {
	switch {
	case slug == "" || len(slug) > MaxSEOSlug || !slugFormat.MatchString(slug):
		return &FieldViolation{
			Field: "slug", Code: SEOCodeInvalidFormat,
			Message: fmt.Sprintf("slug must be 1-%d lowercase letters, digits, and single hyphens", MaxSEOSlug),
		}
	case slices.Contains(ReservedSlugs, slug):
		return &FieldViolation{Field: "slug", Code: SEOCodeSlugReserved, Message: "slug is reserved for a site route"}
	default:
		return nil
	}
}

var slugFormat = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// SEOInput is everything RenderSEO needs; image URLs are already resolved ("" when none).
type SEOInput struct {
	Post            Post
	SEO             SEO
	Tags            []string
	Defaults        SEODefaults
	OGImageURL      string
	TwitterImageURL string
	CoverImageURL   string
}

// SEOMeta is the rendered meta for a post, ready for a frontend to emit as tags.
type SEOMeta struct {
	Title           string            `json:"title"`
	MetaDescription string            `json:"meta_description"`
	MetaKeywords    string            `json:"meta_keywords,omitempty"`
	Canonical       string            `json:"canonical,omitempty"`
	Robots          string            `json:"robots"`
	OpenGraph       map[string]any    `json:"open_graph"`
	Twitter         map[string]string `json:"twitter"`
}

// RenderSEO applies the fallback chains: explicit post SEO, then values derived from the
// post (title, excerpt, content, cover), then site settings. Every text value is plain:
// markup is stripped from derived values and rejected in stored ones.
func RenderSEO(in SEOInput) SEOMeta {
	d := in.Defaults
	post := in.Post

	title := firstNonEmpty(in.SEO.Title, applyTemplate(d.TitleTemplate, PlainText(post.Title), d.SiteName))
	description := firstNonEmpty(in.SEO.Description, summary(post.Excerpt), summary(post.Content), d.Description)
	canonical := firstNonEmpty(in.SEO.CanonicalURL, permalink(d.CanonicalBase, post.Slug))

	ogTitle := firstNonEmpty(in.SEO.OGTitle, in.SEO.Title, PlainText(post.Title))
	ogDescription := firstNonEmpty(in.SEO.OGDescription, description)
	ogImage := firstNonEmpty(in.OGImageURL, in.CoverImageURL, d.ShareImageURL)

	og := map[string]any{"og:type": "article"}
	setAny(og, "og:title", ogTitle)
	setAny(og, "og:description", ogDescription)
	setAny(og, "og:image", ogImage)
	setAny(og, "og:url", firstNonEmpty(in.SEO.OGURL, canonical))
	setAny(og, "og:site_name", d.SiteName)

	if post.PublishedAt != nil {
		og["article:published_time"] = post.PublishedAt.UTC().Format(time.RFC3339)
	}

	if !post.UpdatedAt.IsZero() {
		og["article:modified_time"] = post.UpdatedAt.UTC().Format(time.RFC3339)
	}

	if len(in.Tags) > 0 {
		og["article:tag"] = in.Tags
	}

	card := firstNonEmpty(string(in.SEO.TwitterCard), string(d.TwitterCard), string(TwitterSummaryLarge))
	twitter := map[string]string{"twitter:card": card}
	setString(twitter, "twitter:title", firstNonEmpty(in.SEO.TwitterTitle, ogTitle))
	setString(twitter, "twitter:description", firstNonEmpty(in.SEO.TwitterDescription, ogDescription))
	setString(twitter, "twitter:image", firstNonEmpty(in.TwitterImageURL, ogImage))
	setString(twitter, "twitter:creator", in.SEO.TwitterCreator)

	return SEOMeta{
		Title:           title,
		MetaDescription: description,
		MetaKeywords:    strings.Join(in.SEO.Keywords, ", "),
		Canonical:       canonical,
		Robots:          robots(in.SEO),
		OpenGraph:       og,
		Twitter:         twitter,
	}
}

// SEOPreview is how a post would appear in search results and social cards.
type SEOPreview struct {
	Search   SearchPreview    `json:"search_preview"`
	OG       SocialPreview    `json:"og_preview"`
	Twitter  TwitterPreview   `json:"twitter_preview"`
	Warnings []FieldViolation `json:"warnings"`
}

// SearchPreview is a search-result snippet.
type SearchPreview struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// SocialPreview is an Open Graph card.
type SocialPreview struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	URL         string `json:"url"`
	SiteName    string `json:"site_name"`
}

// TwitterPreview is a Twitter card.
type TwitterPreview struct {
	Card        string `json:"card"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	Creator     string `json:"creator"`
}

// PreviewOf turns rendered meta into editor preview cards.
func PreviewOf(meta SEOMeta, warnings []FieldViolation) SEOPreview {
	og := func(key string) string { s, _ := meta.OpenGraph[key].(string); return s }

	return SEOPreview{
		Search: SearchPreview{Title: meta.Title, URL: meta.Canonical, Description: meta.MetaDescription},
		OG: SocialPreview{
			Title: og("og:title"), Description: og("og:description"), ImageURL: og("og:image"),
			URL: og("og:url"), SiteName: og("og:site_name"),
		},
		Twitter: TwitterPreview{
			Card: meta.Twitter["twitter:card"], Title: meta.Twitter["twitter:title"],
			Description: meta.Twitter["twitter:description"], ImageURL: meta.Twitter["twitter:image"],
			Creator: meta.Twitter["twitter:creator"],
		},
		Warnings: warnings,
	}
}

var (
	markdownImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	markdownLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
	markdownMarks = regexp.MustCompile("[#*_`~>|]+")
)

// PlainText strips HTML tags, Markdown syntax, stray angle brackets, and control
// characters from s and collapses whitespace, so derived meta cannot carry markup.
func PlainText(s string) string {
	s = markdownImage.ReplaceAllString(s, " ")
	s = markdownLink.ReplaceAllString(s, "$1")
	s = htmlTag.ReplaceAllString(s, " ")
	s = markdownMarks.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return -1
		}

		return r
	}, s)

	return collapseSpace(s)
}

func summary(s string) string {
	s = PlainText(s)
	if utf8.RuneCountInString(s) <= derivedDescriptionAt {
		return s
	}

	runes := []rune(s)[:derivedDescriptionAt]
	cut := string(runes)

	if i := strings.LastIndex(cut, " "); i > derivedDescriptionAt/2 {
		cut = cut[:i]
	}

	return strings.TrimRight(cut, " ,.;:") + "…"
}

func applyTemplate(template, title, site string) string {
	if template == "" {
		return title
	}

	return strings.ReplaceAll(strings.ReplaceAll(template, "{title}", title), "{site}", site)
}

func permalink(base, slug string) string {
	if base == "" || slug == "" {
		return ""
	}

	return strings.TrimRight(base, "/") + "/posts/" + slug
}

func robots(seo SEO) string {
	index, follow := "index", "follow"
	if seo.RobotsNoindex {
		index = "noindex"
	}

	if seo.RobotsNofollow {
		follow = "nofollow"
	}

	return index + "," + follow
}

func checkText(s string, maxRunes int) (string, string) {
	if utf8.RuneCountInString(s) > maxRunes {
		return SEOCodeTooLong, fmt.Sprintf("must be at most %d characters", maxRunes)
	}

	if strings.ContainsAny(s, "<>") {
		return SEOCodeMarkup, "must not contain < or >"
	}

	for _, r := range s {
		if unicode.IsControl(r) {
			return SEOCodeInvalidFormat, "must not contain control characters"
		}
	}

	return "", ""
}

func checkSEOURL(raw string, defaults SEODefaults) (string, string) {
	if raw == "" {
		return "", ""
	}

	if len(raw) > MaxSEOURL {
		return SEOCodeTooLong, fmt.Sprintf("must be at most %d characters", MaxSEOURL)
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return SEOCodeInvalidFormat, "must be an absolute http(s) URL without credentials"
	}

	base, err := url.Parse(defaults.CanonicalBase)
	if defaults.CanonicalBase == "" || err != nil {
		return "", ""
	}

	host := strings.ToLower(u.Hostname())
	if host == strings.ToLower(base.Hostname()) || slices.Contains(defaults.AllowedHosts, host) {
		return "", ""
	}

	return SEOCodeHostNotAllowed, "host must be the site's canonical host or listed in seo.canonical_allowed_hosts"
}

func normaliseKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))

	for _, keyword := range in {
		keyword = collapseSpace(keyword)
		key := strings.ToLower(keyword)

		if _, dup := seen[key]; keyword == "" || dup {
			continue
		}

		seen[key] = struct{}{}

		out = append(out, keyword)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

func setAny(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func setString(m map[string]string, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func changedKeys(before, after map[string]any) []string {
	var keys []string

	for k, v := range after {
		if !jsonEqual(before[k], v) {
			keys = append(keys, k)
		}
	}

	for k := range before {
		if _, ok := after[k]; !ok {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	return keys
}

func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)

	return string(ja) == string(jb)
}
