// Package markdown renders user-written markdown to HTML that is safe to embed.
package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
)

// Renderer converts CommonMark to HTML and keeps only an allow-list of elements:
// paragraphs, line breaks, bold, italic, inline and block code, blockquotes, lists,
// and http(s)/mailto links marked rel="nofollow noreferrer". Raw HTML in the input is
// dropped by the markdown step; anything else outside the list (headings, images,
// tables) is reduced to its text by the sanitizer.
type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// New returns a Renderer for comment bodies.
func New() *Renderer {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "em", "code", "pre", "blockquote", "ul", "ol", "li")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.RequireParseableURLs(true)
	policy.AllowRelativeURLs(false)
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)

	return &Renderer{md: goldmark.New(), policy: policy}
}

// NewNewsletter returns a Renderer for newsletter issues written by staff. On top of the
// comment elements it keeps headings, horizontal rules, and https images with alt text; links
// keep noreferrer but drop nofollow, since they are the sender's own.
func NewNewsletter() *Renderer {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "em", "code", "pre", "blockquote", "ul", "ol", "li",
		"h1", "h2", "h3", "h4", "hr")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("src", "alt", "title").OnElements("img")
	policy.AllowURLSchemes("https", "mailto")
	policy.RequireParseableURLs(true)
	policy.AllowRelativeURLs(false)
	policy.RequireNoReferrerOnLinks(true)

	return &Renderer{md: goldmark.New(), policy: policy}
}

// Render returns sanitized HTML for source; it never returns an error because
// invalid markdown still renders as text.
func (r *Renderer) Render(source string) string {
	if source == "" {
		return ""
	}

	var buf bytes.Buffer
	if err := r.md.Convert([]byte(source), &buf); err != nil {
		buf.Reset()
		buf.WriteString(source)
	}

	return r.policy.Sanitize(buf.String())
}
