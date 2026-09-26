package template_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/notification/template"
)

func TestEmailRendersHTMLLayout(t *testing.T) {
	t.Parallel()

	msg, err := template.Render(template.ChannelEmail, template.TypeNewsletterConfirm, template.Data{
		Name: "Ada", Token: "secret-token", ExpiresAt: "Fri, 25 Sep 2026 00:00:00 UTC",
		SiteURL: "https://blog.example", SiteName: "Example Blog", Lists: "Weekly",
	})
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(msg.HTML, "<!doctype html>"))
	require.Contains(t, msg.HTML, "<title>Confirm your subscription to Example Blog</title>")
	require.Contains(t, msg.HTML, ">Example Blog</td>", "site name heads the email")
	require.Contains(t, msg.HTML, `<p style="margin:0 0 16px;">Hi Ada,</p>`)
	require.Contains(t, msg.HTML,
		`<a href="https://blog.example/newsletter/confirm?token=secret-token"`, "the confirm URL is a link")
	require.Contains(t, msg.Body, "secret-token", "the plain-text part is unchanged")
}

func TestEmailHTMLSetsTokenApartAndLinksURLs(t *testing.T) {
	t.Parallel()

	msg, err := template.Render(template.ChannelEmail, template.TypePasswordReset, template.Data{
		Name: "Ada", Email: "ada@example.com", Token: "secret-token",
		ExpiresAt: "2026-09-25T00:00:00Z", PublicURL: "http://127.0.0.1:8080",
	})
	require.NoError(t, err)

	require.Regexp(t, `<code [^>]*>secret-token</code>`, msg.HTML)
	require.Contains(t, msg.HTML, `<a href="http://127.0.0.1:8080/api/v1/auth/password/reset"`)
	require.NotContains(t, msg.HTML, "Example Blog", "no site header without a site name")
}

func TestEmailHTMLTrimsTrailingPunctuationFromLinks(t *testing.T) {
	t.Parallel()

	msg, err := template.RenderTemplate(template.Template{
		Type: "x", Channel: template.ChannelEmail, Subject: "s",
		Body: "Read {{.PostURL}}. Then reply.",
	}, template.Data{PostURL: "https://blog.example/posts/launch"})
	require.NoError(t, err)

	require.Contains(t, msg.HTML, `<a href="https://blog.example/posts/launch" style="color:#1a73e8;word-break:break-all;">https://blog.example/posts/launch</a>. Then reply.`)
}

func TestEmailHTMLEscapesStoredContent(t *testing.T) {
	t.Parallel()

	msg, err := template.RenderTemplate(template.Template{
		Type: "x", Channel: template.ChannelEmail, Subject: `<b>Hi</b>`,
		Body: `<script>alert(1)</script> {{.Name}} javascript:alert(1) "https://x.example/a"onmouseover="x"`,
	}, template.Data{Name: `<img src=x onerror=alert(1)>`})
	require.NoError(t, err)

	require.NotContains(t, msg.HTML, "<script>")
	require.NotContains(t, msg.HTML, "<img")
	require.NotContains(t, msg.HTML, "<b>Hi</b>")
	require.NotContains(t, msg.HTML, `href="javascript:`)
	require.NotContains(t, msg.HTML, `"onmouseover`)
	require.Contains(t, msg.HTML, "&lt;script&gt;")
	require.Contains(t, msg.HTML, `<a href="https://x.example/a"`)
}

func TestHTMLIsEmailOnly(t *testing.T) {
	t.Parallel()

	for _, channel := range []template.Channel{template.ChannelWeb, template.ChannelSSE} {
		msg, err := template.Render(channel, template.TypePasswordChanged, template.Data{Name: "Ada"})
		require.NoError(t, err)
		require.Empty(t, msg.HTML, channel)
	}
}
