package mail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/resend/resend-go/v3"
	"github.com/turahe/blog-api/internal/core/notification/ports"
)

// Resend delivers messages through the Resend HTTP API.
type Resend struct {
	client *resend.Client
	from   string
}

var _ ports.Mailer = (*Resend)(nil)

// NewResend returns a Resend mailer. from is the sender header and must be on a domain
// verified in Resend. A nil httpClient uses a 10 second timeout.
func NewResend(apiKey, from string, httpClient *http.Client) (*Resend, error) {
	apiKey = strings.TrimSpace(apiKey)
	from = strings.TrimSpace(from)

	if apiKey == "" {
		return nil, errors.New("resend api key is required")
	}

	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("resend from address: %w", err)
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	withStatus := *httpClient
	withStatus.Transport = statusTransport{base: httpClient.Transport}

	return &Resend{client: resend.NewCustomClient(&withStatus, apiKey), from: from}, nil
}

// Send delivers one plain-text message.
func (r *Resend) Send(ctx context.Context, msg ports.Message) error {
	recipient, subject, err := checkMessage(msg)
	if err != nil {
		return err
	}

	return r.SendEmail(ctx, &resend.SendEmailRequest{
		From: r.from, To: []string{recipient}, Subject: subject, Text: msg.Text,
	}, "")
}

// From returns the configured sender header value.
func (r *Resend) From() string { return r.from }

// SendEmail posts req. A non-empty idempotencyKey makes Resend drop repeats of the same send
// for 24 hours. Failures are *ResendError.
func (r *Resend) SendEmail(ctx context.Context, req *resend.SendEmailRequest, idempotencyKey string) error {
	status := new(int)

	_, err := r.client.Emails.SendWithOptions(context.WithValue(ctx, statusKey{}, status), req,
		&resend.SendEmailOptions{IdempotencyKey: idempotencyKey})
	if err != nil {
		return &ResendError{Status: *status, Err: err}
	}

	return nil
}

// ResendError is a failed Resend API call. Status is 0 when no response arrived.
type ResendError struct {
	Status int
	Err    error
}

func (e *ResendError) Error() string {
	reason := strings.TrimPrefix(e.Err.Error(), "[ERROR]: ")
	if e.Status == 0 {
		return "resend: " + reason
	}

	return fmt.Sprintf("resend answered %d: %s", e.Status, reason)
}

func (e *ResendError) Unwrap() error { return e.Err }

// Permanent reports whether a retry cannot succeed: any 4xx except a timeout, an idempotency
// conflict with a request still in flight, or a rate limit.
func (e *ResendError) Permanent() bool {
	switch e.Status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return false
	default:
		return e.Status >= 400 && e.Status < 500
	}
}

type statusKey struct{}

// statusTransport records the response status in the request context's *int, because the
// SDK drops it from most errors.
type statusTransport struct {
	base http.RoundTripper
}

func (t statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(req)
	if status, ok := req.Context().Value(statusKey{}).(*int); ok && resp != nil {
		*status = resp.StatusCode
	}

	return resp, err
}
