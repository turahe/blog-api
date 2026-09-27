package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

func TestPreview(t *testing.T) {
	t.Parallel()

	issue := domain.Issue{Subject: "Launch notes", Preheader: "What shipped", BodyMarkdown: "Hello <b>bold</b> world"}

	tests := []struct {
		name      string
		edit      func(*Deps, *Config)
		setup     func(f *fixture)
		wantErr   error
		wantHTML  []string
		wantText  []string
		avoidHTML []string
		avoidText []string
	}{
		{
			name: "renders with placeholder links",
			wantHTML: []string{
				"<title>Launch notes</title>", "What shipped", "<p>Hello <b>bold</b> world</p>",
				"subscribed to Example Blog", `href="https://blog.example.test/newsletter/unsubscribe?token=preview"`,
				`href="https://blog.example.test/newsletter/preferences?token=preview"`, "<br>1 Example Street",
			},
			wantText: []string{
				"Launch notes\n\nHello bold world\n\n--\nYou receive this email because you subscribed to Example Blog.\n" +
					"Unsubscribe: https://blog.example.test/newsletter/unsubscribe?token=preview\n" +
					"Manage preferences: https://blog.example.test/newsletter/preferences?token=preview\n1 Example Street\n",
			},
		},
		{
			name:     "without links the sender name signs the footer",
			edit:     func(d *Deps, _ *Config) { d.Links = nil },
			wantHTML: []string{"subscribed to Example.", `href="/newsletter/unsubscribe?token=preview"`},
			wantText: []string{"Manage preferences: /newsletter/preferences?token=preview"},
		},
		{
			name:     "a blank site name falls back to the sender name",
			edit:     func(d *Deps, _ *Config) { d.Links = fakeLinks{site: "https://blog.example.test"} },
			wantHTML: []string{"subscribed to Example."},
		},
		{
			name:      "without markdown the body is empty",
			edit:      func(d *Deps, _ *Config) { d.Markdown = nil },
			avoidHTML: []string{"Hello"},
			wantText:  []string{"Hello bold world"},
		},
		{
			name:      "no postal address line when unset",
			setup:     withoutPostalAddress,
			avoidHTML: []string{"<br>1 Example Street"},
			avoidText: []string{"1 Example Street"},
		},
		{name: "config unavailable", setup: func(f *fixture) { f.repo.failOn("Config", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var edits []func(*Deps, *Config)
			if tt.edit != nil {
				edits = append(edits, tt.edit)
			}

			f := newFixture(edits...)
			if tt.setup != nil {
				tt.setup(f)
			}

			html, text, err := f.svc.Preview(t.Context(), issue)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			for _, want := range tt.wantHTML {
				assert.Contains(t, html, want)
			}

			for _, want := range tt.wantText {
				assert.Contains(t, text, want)
			}

			for _, avoid := range tt.avoidHTML {
				assert.NotContains(t, html, avoid)
			}

			for _, avoid := range tt.avoidText {
				assert.NotContains(t, text, avoid)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"## Hi\n\nRead [more](https://x.test).": "## Hi\n\nRead [more](https://x.test).",
		"<script>alert(1)</script>Text":         "Text",
		"a <b>bold</b> <STYLE>p{}</style>word":  "a bold word",
		"1 < 2 and 3 > 2":                       "1 < 2 and 3 > 2",
	}

	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Errorf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}
