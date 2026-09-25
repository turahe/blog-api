package service

import (
	"bytes"
	"context"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// links are one recipient's raw tokens.
type links struct {
	Unsubscribe string
	Preferences string
}

type footer struct {
	SiteName       string
	UnsubscribeURL string
	PreferencesURL string
	PostalAddress  string
}

var issueHTML = template.Must(template.New("issue").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:#f5f5f5;">
<span style="display:none;max-height:0;overflow:hidden;">{{.Preheader}}</span>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f5f5f5;"><tr><td align="center" style="padding:16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#ffffff;border-radius:6px;">
<tr><td style="padding:24px;font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.5;color:#222222;">{{.Body}}</td></tr>
<tr><td style="padding:16px 24px;border-top:1px solid #eeeeee;font-family:Arial,Helvetica,sans-serif;font-size:12px;line-height:1.5;color:#666666;">
You receive this email because you subscribed to {{.Footer.SiteName}}.<br>
<a href="{{.Footer.UnsubscribeURL}}" style="color:#666666;">Unsubscribe</a> &middot;
<a href="{{.Footer.PreferencesURL}}" style="color:#666666;">Manage preferences</a>
{{- if .Footer.PostalAddress}}<br>{{.Footer.PostalAddress}}{{end}}
</td></tr></table></td></tr></table></body></html>
`))

type issueView struct {
	Subject   string
	Preheader string
	Body      template.HTML
	Footer    footer
}

// compose renders one recipient's email.
func (s *Service) compose(
	ctx context.Context, issue domain.Issue, cfg domain.Config, body string, r domain.Recipient, tokens links,
) ports.Email {
	f := s.footer(ctx, cfg, tokens)
	email := ports.Email{
		To: r.Email, Subject: issue.Subject, FromName: cfg.FromName, FromEmail: cfg.FromEmail, ReplyTo: cfg.ReplyTo,
		Text: plainText(issue, f), IdempotencyKey: r.DeliveryID.String(),
		Headers: map[string]string{"List-Unsubscribe-Post": "List-Unsubscribe=One-Click"},
	}

	if s.Links != nil {
		oneClick := strings.TrimRight(s.Links.APIURL(), "/") + "/api/v1/newsletter/unsubscribe?token=" + url.QueryEscape(tokens.Unsubscribe)
		email.Headers["List-Unsubscribe"] = "<" + oneClick + ">"
	}

	if r.Format != domain.FormatPlaintext {
		email.HTML = renderHTML(issue, body, f)
	}

	return email
}

// Preview renders an issue as a subscriber would see it, with placeholder links.
func (s *Service) Preview(ctx context.Context, issue domain.Issue) (html, text string, err error) {
	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return "", "", err
	}

	body := ""
	if s.Markdown != nil {
		body = s.Markdown.Render(issue.BodyMarkdown)
	}

	f := s.footer(ctx, cfg, links{Unsubscribe: "preview", Preferences: "preview"})

	return renderHTML(issue, body, f), plainText(issue, f), nil
}

func (s *Service) footer(ctx context.Context, cfg domain.Config, tokens links) footer {
	site, name := "", cfg.FromName

	if s.Links != nil {
		site = strings.TrimRight(s.Links.SiteURL(ctx), "/")
		if n := s.Links.SiteName(ctx); n != "" {
			name = n
		}
	}

	return footer{
		SiteName:       name,
		UnsubscribeURL: site + "/newsletter/unsubscribe?token=" + url.QueryEscape(tokens.Unsubscribe),
		PreferencesURL: site + "/newsletter/preferences?token=" + url.QueryEscape(tokens.Preferences),
		PostalAddress:  cfg.PostalAddress,
	}
}

func renderHTML(issue domain.Issue, body string, f footer) string {
	var buf bytes.Buffer

	// The template and its inputs are fixed shapes; Execute fails only on writer errors.
	_ = issueHTML.Execute(&buf, issueView{
		Subject: issue.Subject, Preheader: issue.Preheader,
		Body:   template.HTML(body), //nolint:gosec // body is sanitized by the markdown renderer
		Footer: f,
	})

	return buf.String()
}

func plainText(issue domain.Issue, f footer) string {
	var b strings.Builder

	b.WriteString(issue.Subject)
	b.WriteString("\n\n")
	b.WriteString(stripHTML(issue.BodyMarkdown))
	b.WriteString("\n\n--\nYou receive this email because you subscribed to ")
	b.WriteString(f.SiteName)
	b.WriteString(".\nUnsubscribe: ")
	b.WriteString(f.UnsubscribeURL)
	b.WriteString("\nManage preferences: ")
	b.WriteString(f.PreferencesURL)

	if f.PostalAddress != "" {
		b.WriteString("\n")
		b.WriteString(f.PostalAddress)
	}

	b.WriteString("\n")

	return b.String()
}

var (
	rawBlock = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)\s*>`)
	rawTag   = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
)

// stripHTML removes raw HTML from Markdown so the plaintext part carries only prose.
func stripHTML(md string) string {
	return strings.TrimSpace(rawTag.ReplaceAllString(rawBlock.ReplaceAllString(md, ""), ""))
}
