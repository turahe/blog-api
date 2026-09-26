// Package newsletterprovider delivers newsletter issues through the built-in SMTP mailer, the
// Resend API, or a custom HTTP gateway signed with HMAC, and verifies that gateway's bounce
// webhooks.
package newsletterprovider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// RawSender sends a complete message; *mail.SMTP implements it.
type RawSender interface {
	SendRaw(ctx context.Context, recipient string, body []byte) error
	From() string
}

// SMTP sends issues as multipart/alternative email with RFC 8058 unsubscribe headers.
type SMTP struct {
	raw RawSender
	now func() time.Time
}

var _ ports.Sender = (*SMTP)(nil)

// NewSMTP returns an SMTP sender over raw.
func NewSMTP(raw RawSender) *SMTP {
	return &SMTP{raw: raw, now: time.Now}
}

// Send renders email as MIME and delivers it. A malformed recipient is a permanent failure.
func (s *SMTP) Send(ctx context.Context, email ports.Email) error {
	recipient, err := mail.ParseAddress(email.To)
	if err != nil {
		return fmt.Errorf("%w: recipient address", domain.ErrPermanent)
	}

	body, err := s.compose(email, recipient.Address)
	if err != nil {
		return err
	}

	return s.raw.SendRaw(ctx, recipient.Address, body)
}

func (s *SMTP) compose(email ports.Email, recipient string) ([]byte, error) {
	from, err := s.from(email)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"From":         from,
		"To":           recipient,
		"Subject":      mime.QEncoding.Encode("utf-8", email.Subject),
		"Date":         s.now().UTC().Format(time.RFC1123Z),
		"MIME-Version": "1.0",
	}

	if email.ReplyTo != "" {
		headers["Reply-To"] = email.ReplyTo
	}

	for name, value := range email.Headers {
		headers[textproto.CanonicalMIMEHeaderKey(name)] = value
	}

	for name, value := range headers {
		if strings.ContainsAny(name+value, "\r\n") {
			return nil, fmt.Errorf("%w: header %s has a line break", domain.ErrPermanent, name)
		}
	}

	var buf bytes.Buffer

	if email.HTML == "" {
		headers["Content-Type"] = "text/plain; charset=UTF-8"
		headers["Content-Transfer-Encoding"] = "quoted-printable"
		writeHeaders(&buf, headers)

		if err := writeQP(&buf, email.Text); err != nil {
			return nil, err
		}

		return buf.Bytes(), nil
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	headers["Content-Type"] = `multipart/alternative; boundary="` + boundary + `"`
	writeHeaders(&buf, headers)

	for _, part := range []struct{ kind, body string }{{"text/plain", email.Text}, {"text/html", email.HTML}} {
		fmt.Fprintf(&buf, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
			boundary, part.kind)

		if err := writeQP(&buf, part.body); err != nil {
			return nil, err
		}

		buf.WriteString("\r\n")
	}

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	return buf.Bytes(), nil
}

// from builds the From header. The envelope sender stays the mailer's address so SPF keeps
// passing.
func (s *SMTP) from(email ports.Email) (string, error) {
	return fromHeader(s.raw.From(), email)
}

// fromHeader returns the issue's configured sender name and address, each falling back to the
// transport's sender.
func fromHeader(transportFrom string, email ports.Email) (string, error) {
	fallback, err := mail.ParseAddress(transportFrom)
	if err != nil {
		return "", fmt.Errorf("mailer from address: %w", err)
	}

	addr := mail.Address{Name: fallback.Name, Address: fallback.Address}
	if email.FromEmail != "" {
		addr.Address = email.FromEmail
	}

	if email.FromName != "" {
		addr.Name = email.FromName
	}

	return addr.String(), nil
}

func writeHeaders(buf *bytes.Buffer, headers map[string]string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		buf.WriteString(name + ": " + headers[name] + "\r\n")
	}

	buf.WriteString("\r\n")
}

func writeQP(buf *bytes.Buffer, text string) error {
	w := quotedprintable.NewWriter(buf)
	if _, err := w.Write([]byte(text)); err != nil {
		return err
	}

	return w.Close()
}

func newBoundary() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("mime boundary: " + err.Error())
	}

	return "nl-" + hex.EncodeToString(b), nil
}
