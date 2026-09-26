package template

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"regexp"
	"strings"
)

//go:embed email_layout.html
var layoutFS embed.FS

var emailLayout = htmltemplate.Must(htmltemplate.ParseFS(layoutFS, "email_layout.html"))

// segment is a run of text in one line of an email body: plain text, a link, or the token.
type segment struct {
	Text string
	Href string
	Code bool
}

type emailView struct {
	Subject    string
	SiteName   string
	Paragraphs [][][]segment
}

var bodyURL = regexp.MustCompile(`https?://[^\s<>"']+`)

// emailHTML wraps a rendered plain-text email body in the HTML layout. Blank lines separate
// paragraphs, http(s) URLs become links, and token is set apart as code. html/template
// escapes every value, so stored templates cannot inject markup.
func emailHTML(subject, body, token, siteName string) (string, error) {
	view := emailView{Subject: subject, SiteName: siteName, Paragraphs: paragraphs(body, token)}

	var buf bytes.Buffer
	if err := emailLayout.ExecuteTemplate(&buf, "layout", view); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func paragraphs(body, token string) [][][]segment {
	var out [][][]segment

	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		var lines [][]segment

		for _, line := range strings.Split(block, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, segments(line, token))
			}
		}

		if len(lines) > 0 {
			out = append(out, lines)
		}
	}

	return out
}

// segments splits one line into text, link, and token runs.
func segments(line, token string) []segment {
	var out []segment

	rest := line
	for rest != "" {
		loc := bodyURL.FindStringIndex(rest)
		if loc == nil {
			return append(out, tokenSegments(rest, token)...)
		}

		href := strings.TrimRight(rest[loc[0]:loc[1]], ".,;:!?)")
		out = append(out, tokenSegments(rest[:loc[0]], token)...)
		out = append(out, segment{Text: href, Href: href})
		rest = rest[loc[0]+len(href):]
	}

	return out
}

func tokenSegments(text, token string) []segment {
	var out []segment

	for text != "" {
		before, after, found := "", "", false
		if token != "" {
			before, after, found = strings.Cut(text, token)
		}

		if !found {
			return append(out, segment{Text: text})
		}

		if before != "" {
			out = append(out, segment{Text: before})
		}

		out = append(out, segment{Text: token, Code: true})
		text = after
	}

	return out
}
