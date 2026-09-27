package markdown

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewNewsletter(t *testing.T) {
	t.Parallel()

	r := NewNewsletter()

	tests := []struct {
		name        string
		source      string
		contains    []string
		notContains []string
	}{
		{
			name:     "headings and rules kept",
			source:   "# One\n\n## Two\n\n### Three\n\n#### Four\n\n---",
			contains: []string{"<h1>One</h1>", "<h2>Two</h2>", "<h3>Three</h3>", "<h4>Four</h4>", "<hr"},
		},
		{
			name:        "deeper headings reduced to text",
			source:      "##### Five",
			contains:    []string{"Five"},
			notContains: []string{"<h5"},
		},
		{
			name:     "https image keeps src and alt",
			source:   `![logo](https://example.com/a.png "Title")`,
			contains: []string{`<img src="https://example.com/a.png" alt="logo" title="Title"`},
		},
		{
			name:        "http image loses src",
			source:      "![logo](http://example.com/a.png)",
			contains:    []string{`<img alt="logo">`},
			notContains: []string{"src=", "http://example.com"},
		},
		{
			name:        "links are noreferrer without nofollow",
			source:      "[site](https://example.com/page)",
			contains:    []string{`href="https://example.com/page"`, `rel="noreferrer"`},
			notContains: []string{"nofollow"},
		},
		{
			name:     "mailto link kept",
			source:   "[mail](mailto:a@example.com)",
			contains: []string{`href="mailto:a@example.com"`},
		},
		{
			name:        "raw script dropped",
			source:      "hi <script>alert(1)</script>",
			notContains: []string{"<script"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			html := r.Render(tt.source)
			for _, want := range tt.contains {
				require.Contains(t, html, want)
			}

			for _, bad := range tt.notContains {
				require.NotContains(t, html, bad)
			}
		})
	}
}

func TestRenderAllowsBasicFormatting(t *testing.T) {
	t.Parallel()

	html := New().Render("**bold** _italic_ `code`\n\n> quote\n\n- one\n- two\n\n1. first\n\n```\nblock\n```")

	for _, want := range []string{
		"<strong>bold</strong>", "<em>italic</em>", "<code>code</code>",
		"<blockquote>", "<ul>", "<li>one</li>", "<ol>", "<pre><code>block\n</code></pre>",
	} {
		require.Contains(t, html, want)
	}
}

func TestRenderLinksAreNofollow(t *testing.T) {
	t.Parallel()

	html := New().Render("[site](https://example.com/page)")

	require.Contains(t, html, `href="https://example.com/page"`)
	require.Contains(t, html, `rel="nofollow noreferrer"`)
}

func TestRenderStripsDangerousInput(t *testing.T) {
	t.Parallel()

	r := New()

	cases := map[string]string{
		"raw script":         "hi <script>alert(1)</script>",
		"inline handler":     `<img src=x onerror="alert(1)">`,
		"javascript link":    "[x](javascript:alert(1))",
		"data link":          "[x](data:text/html;base64,PHNjcmlwdD4=)",
		"iframe":             `<iframe src="https://evil.test"></iframe>`,
		"autolink js":        "<javascript:alert(1)>",
		"html in link title": `[x](https://a.test "<script>")`,
	}

	for name, source := range cases {
		html := r.Render(source)
		for _, bad := range []string{"<script", "onerror=", `href="javascript:`, `href="data:`, "<iframe", "<img"} {
			require.NotContains(t, html, bad, name)
		}
	}
}

func TestRenderReducesDisallowedElementsToText(t *testing.T) {
	t.Parallel()

	html := New().Render("# Heading\n\n![alt](https://example.com/a.png)\n\n---")

	require.NotContains(t, html, "<h1")
	require.NotContains(t, html, "<img")
	require.NotContains(t, html, "<hr")
	require.Contains(t, html, "Heading")
}

func TestRenderEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, New().Render(""))
}
