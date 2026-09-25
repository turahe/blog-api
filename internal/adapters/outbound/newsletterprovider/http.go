package newsletterprovider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// Signature headers on requests to and from the gateway. The signature is
// v1=hex(HMAC-SHA256(secret, timestamp + "." + body)).
const (
	HeaderEvent     = "X-Newsletter-Event"
	HeaderTimestamp = "X-Newsletter-Timestamp"
	HeaderSignature = "X-Newsletter-Signature"

	// SignatureTolerance is how far a webhook timestamp may be from now.
	SignatureTolerance = 5 * time.Minute

	eventDelivery = "newsletter.delivery"
	eventContact  = "newsletter.contact"
	httpTimeout   = 15 * time.Second
)

// HTTP posts deliveries and contact changes as signed JSON to a gateway.
type HTTP struct {
	endpoint string
	secret   []byte
	client   *http.Client
	now      func() time.Time
}

var (
	_ ports.Sender      = (*HTTP)(nil)
	_ ports.ContactSync = (*HTTP)(nil)
)

// NewHTTP returns a gateway client. A nil client uses one with a 15 second timeout.
func NewHTTP(endpoint, secret string, client *http.Client) *HTTP {
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}

	return &HTTP{endpoint: endpoint, secret: []byte(secret), client: client, now: time.Now}
}

type deliveryPayload struct {
	DeliveryID string            `json:"delivery_id"`
	To         string            `json:"to"`
	Subject    string            `json:"subject"`
	Text       string            `json:"text"`
	HTML       string            `json:"html,omitempty"`
	FromName   string            `json:"from_name,omitempty"`
	FromEmail  string            `json:"from_email,omitempty"`
	ReplyTo    string            `json:"reply_to,omitempty"`
	Headers    map[string]string `json:"headers"`
}

// Send posts one delivery. The gateway answers 2xx once it accepted the message.
func (h *HTTP) Send(ctx context.Context, email ports.Email) error {
	return h.post(ctx, eventDelivery, email.IdempotencyKey, deliveryPayload{
		DeliveryID: email.IdempotencyKey, To: email.To, Subject: email.Subject, Text: email.Text, HTML: email.HTML,
		FromName: email.FromName, FromEmail: email.FromEmail, ReplyTo: email.ReplyTo, Headers: email.Headers,
	})
}

type contactPayload struct {
	ID        string    `json:"id"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	Status    string    `json:"status"`
	Format    string    `json:"format"`
	Lists     []string  `json:"lists"`
	ChangedAt time.Time `json:"changed_at"`
}

// SyncContact posts a subscriber's current state. Erased contacts carry only their id.
func (h *HTTP) SyncContact(ctx context.Context, c ports.Contact) error {
	lists := c.Lists
	if lists == nil {
		lists = []string{}
	}

	return h.post(ctx, eventContact, c.ID.String()+"@"+strconv.FormatInt(c.ChangedAt.UnixNano(), 10), contactPayload{
		ID: c.ID.String(), Email: c.Email, Name: c.Name, Status: string(c.Status), Format: string(c.Format),
		Lists: lists, ChangedAt: c.ChangedAt.UTC(),
	})
}

// post sends a signed request. 4xx answers other than 408 and 429 are permanent; the response
// body is never read into errors because gateways often echo the address.
func (h *HTTP) post(ctx context.Context, event, idempotencyKey string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s: %w", event, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: gateway request: %w", domain.ErrPermanent, err)
	}

	timestamp := strconv.FormatInt(h.now().Unix(), 10)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderEvent, event)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, Sign(h.secret, timestamp, body))
	req.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s gateway: %w", event, err)
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s gateway answered %d", domain.ErrPermanent, event, resp.StatusCode)
	default:
		return fmt.Errorf("%s gateway answered %d", event, resp.StatusCode)
	}
}

// Sign returns the signature header value for timestamp and body.
func Sign(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)

	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Verifier checks the signature on gateway webhooks.
type Verifier struct {
	secret []byte
	now    func() time.Time
}

// NewVerifier returns a Verifier for secret.
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret), now: time.Now}
}

// Verify accepts body when signature matches and timestamp is within SignatureTolerance.
func (v *Verifier) Verify(timestamp, signature string, body []byte) error {
	sec, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return domain.ErrSignature
	}

	if d := v.now().Sub(time.Unix(sec, 0)); d > SignatureTolerance || d < -SignatureTolerance {
		return domain.ErrSignature
	}

	if !hmac.Equal([]byte(strings.TrimSpace(signature)), []byte(Sign(v.secret, timestamp, body))) {
		return domain.ErrSignature
	}

	return nil
}
