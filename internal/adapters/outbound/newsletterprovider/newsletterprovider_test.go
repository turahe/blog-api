package newsletterprovider_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/newsletterprovider"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

const secret = "0123456789abcdef0123456789abcdef"

type rawSender struct {
	recipient string
	body      []byte
}

func (r *rawSender) SendRaw(_ context.Context, recipient string, body []byte) error {
	r.recipient, r.body = recipient, body
	return nil
}

func (*rawSender) From() string { return "Blog <mailer@example.test>" }

func email() ports.Email {
	return ports.Email{
		To: "reader@example.test", Subject: "Hello ünïcode", Text: "Plain body", HTML: "<p>HTML body</p>",
		FromName: "Turahe Blog", FromEmail: "news@example.test", ReplyTo: "hello@example.test",
		Headers: map[string]string{
			"List-Unsubscribe":      "<https://api.example.test/api/v1/newsletter/unsubscribe?token=abc>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
		IdempotencyKey: uuid.NewString(),
	}
}

func TestSMTPSendsMultipartWithUnsubscribeHeaders(t *testing.T) {
	t.Parallel()

	raw := &rawSender{}
	require.NoError(t, newsletterprovider.NewSMTP(raw).Send(t.Context(), email()))
	require.Equal(t, "reader@example.test", raw.recipient)

	msg, err := netmail.ReadMessage(strings.NewReader(string(raw.body)))
	require.NoError(t, err)

	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Hello ünïcode", subject)
	require.Equal(t, `"Turahe Blog" <news@example.test>`, msg.Header.Get("From"))
	require.Equal(t, "hello@example.test", msg.Header.Get("Reply-To"))
	require.Equal(t, "List-Unsubscribe=One-Click", msg.Header.Get("List-Unsubscribe-Post"))
	require.Contains(t, msg.Header.Get("List-Unsubscribe"), "token=abc")

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)

	reader := multipart.NewReader(msg.Body, params["boundary"])

	var kinds []string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		require.NoError(t, err)

		kinds = append(kinds, strings.SplitN(part.Header.Get("Content-Type"), ";", 2)[0])
	}

	require.Equal(t, []string{"text/plain", "text/html"}, kinds)
}

func TestSMTPPlaintextOnly(t *testing.T) {
	t.Parallel()

	raw := &rawSender{}
	e := email()
	e.HTML = ""
	require.NoError(t, newsletterprovider.NewSMTP(raw).Send(t.Context(), e))

	msg, err := netmail.ReadMessage(strings.NewReader(string(raw.body)))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(msg.Header.Get("Content-Type"), "text/plain"))

	body, err := io.ReadAll(msg.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "Plain body")
}

func TestSMTPRejectsHeaderInjectionAsPermanent(t *testing.T) {
	t.Parallel()

	e := email()
	e.ReplyTo = "a@example.test\r\nBcc: victim@example.test"
	err := newsletterprovider.NewSMTP(&rawSender{}).Send(t.Context(), e)
	require.ErrorIs(t, err, domain.ErrPermanent)

	e = email()
	e.To = "not an address"
	require.ErrorIs(t, newsletterprovider.NewSMTP(&rawSender{}).Send(t.Context(), e), domain.ErrPermanent)
}

func TestHTTPSignsDeliveries(t *testing.T) {
	t.Parallel()

	var (
		gotHeaders http.Header
		gotBody    []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)

		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	e := email()
	require.NoError(t, newsletterprovider.NewHTTP(server.URL, secret, server.Client()).Send(t.Context(), e))

	require.Equal(t, "newsletter.delivery", gotHeaders.Get(newsletterprovider.HeaderEvent))
	require.Equal(t, e.IdempotencyKey, gotHeaders.Get("Idempotency-Key"))
	require.NoError(t, newsletterprovider.NewVerifier(secret).Verify(
		gotHeaders.Get(newsletterprovider.HeaderTimestamp), gotHeaders.Get(newsletterprovider.HeaderSignature), gotBody))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	require.Equal(t, "reader@example.test", payload["to"])
	require.Equal(t, e.IdempotencyKey, payload["delivery_id"])
}

func TestHTTPClassifiesGatewayAnswers(t *testing.T) {
	t.Parallel()

	for status, permanent := range map[int]bool{
		http.StatusBadRequest: true, http.StatusUnprocessableEntity: true,
		http.StatusTooManyRequests: false, http.StatusRequestTimeout: false, http.StatusBadGateway: false,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		err := newsletterprovider.NewHTTP(server.URL, secret, server.Client()).Send(t.Context(), email())
		server.Close()

		require.Error(t, err, status)
		require.Equal(t, permanent, err != nil && strings.Contains(err.Error(), domain.ErrPermanent.Error()), status)
	}
}

func TestHTTPSyncsErasedContactWithoutAddress(t *testing.T) {
	t.Parallel()

	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	id := uuid.New()
	require.NoError(t, newsletterprovider.NewHTTP(server.URL, secret, server.Client()).SyncContact(t.Context(),
		ports.Contact{ID: id, Status: domain.StatusErased, ChangedAt: time.Now()}))

	require.Equal(t, id.String(), payload["id"])
	require.Equal(t, "erased", payload["status"])
	require.NotContains(t, payload, "email")
	require.Equal(t, []any{}, payload["lists"])
}

func TestVerifierRejectsBadSignatures(t *testing.T) {
	t.Parallel()

	verifier := newsletterprovider.NewVerifier(secret)
	body := []byte(`{"events":[]}`)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)

	require.NoError(t, verifier.Verify(now, newsletterprovider.Sign([]byte(secret), now, body), body))
	require.ErrorIs(t, verifier.Verify(now, newsletterprovider.Sign([]byte("other-secret"), now, body), body), domain.ErrSignature)
	require.ErrorIs(t, verifier.Verify(now, newsletterprovider.Sign([]byte(secret), now, body), []byte(`{}`)), domain.ErrSignature)
	require.ErrorIs(t, verifier.Verify(stale, newsletterprovider.Sign([]byte(secret), stale, body), body), domain.ErrSignature)
	require.ErrorIs(t, verifier.Verify("not-a-number", "v1=00", body), domain.ErrSignature)
}
