package newsletterprovider

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/resend/resend-go/v3"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// ResendClient posts one email to the Resend API; *mail.Resend implements it.
type ResendClient interface {
	SendEmail(ctx context.Context, req *resend.SendEmailRequest, idempotencyKey string) error
	From() string
}

// Resend sends issues through the Resend API with RFC 8058 unsubscribe headers. The delivery
// id is the idempotency key, so a retried delivery is not sent twice.
type Resend struct {
	client ResendClient
}

var _ ports.Sender = (*Resend)(nil)

// NewResend returns a Resend sender over client.
func NewResend(client ResendClient) *Resend {
	return &Resend{client: client}
}

// permanentError is implemented by *mail.ResendError.
type permanentError interface {
	error
	Permanent() bool
}

// Send delivers email. A malformed recipient or header, or a request Resend rejects, is a
// permanent failure.
func (r *Resend) Send(ctx context.Context, email ports.Email) error {
	recipient, err := mail.ParseAddress(email.To)
	if err != nil {
		return fmt.Errorf("%w: recipient address", domain.ErrPermanent)
	}

	from, err := fromHeader(r.client.From(), email)
	if err != nil {
		return err
	}

	for name, value := range email.Headers {
		if strings.ContainsAny(name+value, "\r\n") {
			return fmt.Errorf("%w: header %s has a line break", domain.ErrPermanent, name)
		}
	}

	err = r.client.SendEmail(ctx, &resend.SendEmailRequest{
		From: from, To: []string{recipient.Address}, Subject: email.Subject, Text: email.Text, Html: email.HTML,
		ReplyTo: email.ReplyTo, Headers: email.Headers,
	}, email.IdempotencyKey)
	if err == nil {
		return nil
	}

	if rejected, ok := errors.AsType[permanentError](err); ok && rejected.Permanent() {
		return fmt.Errorf("%w: %w", domain.ErrPermanent, err)
	}

	return err
}
