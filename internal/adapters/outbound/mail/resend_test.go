package mail_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/resend/resend-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/mail"
	"github.com/turahe/blog-api/internal/core/notification/ports"
)

// toServer sends every request to the test server, whatever host the SDK targets.
type toServer struct{ target *url.URL }

func (t toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = t.target.Scheme, t.target.Host

	return http.DefaultTransport.RoundTrip(req)
}

type resendCall struct {
	auth, idempotencyKey string
	body                 map[string]any
}

func newResend(t *testing.T, status int, reply string) (*mail.Resend, *resendCall, *atomic.Int32) {
	t.Helper()

	call, hits := &resendCall{}, &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		call.auth, call.idempotencyKey = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&call.body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	require.NoError(t, err)

	sender, err := mail.NewResend("re_test", "Blog <blog@example.test>", &http.Client{Transport: toServer{target: target}})
	require.NoError(t, err)

	return sender, call, hits
}

func TestResendSend(t *testing.T) {
	t.Parallel()

	sender, call, _ := newResend(t, http.StatusOK, `{"id":"email-1"}`)

	err := sender.Send(t.Context(), ports.Message{
		To: "Ada <ada@example.com>", Subject: " Reset your password ", Text: "token stays in the body",
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer re_test", call.auth)
	require.Empty(t, call.idempotencyKey)
	require.Equal(t, "Blog <blog@example.test>", call.body["from"])
	require.Equal(t, []any{"ada@example.com"}, call.body["to"])
	require.Equal(t, "Reset your password", call.body["subject"])
	require.Equal(t, "token stays in the body", call.body["text"])
	require.NotContains(t, call.body, "html")
}

func TestResendSendEmailSetsIdempotencyKey(t *testing.T) {
	t.Parallel()

	sender, call, _ := newResend(t, http.StatusOK, `{"id":"email-1"}`)

	err := sender.SendEmail(t.Context(), &resend.SendEmailRequest{
		From: sender.From(), To: []string{"ada@example.com"}, Subject: "Issue", Html: "<p>hi</p>",
	}, "delivery-1")
	require.NoError(t, err)
	require.Equal(t, "delivery-1", call.idempotencyKey)
}

func TestResendErrorsKeepStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		status    int
		permanent bool
		message   string
	}{
		"invalid request": {http.StatusUnprocessableEntity, true, "resend answered 422: The from domain is not verified."},
		"forbidden":       {http.StatusForbidden, true, "resend answered 403: The from domain is not verified."},
		"rate limited":    {http.StatusTooManyRequests, false, "resend answered 429: "},
		"conflict":        {http.StatusConflict, false, "resend answered 409: The from domain is not verified."},
		"server error":    {http.StatusInternalServerError, false, "resend answered 500: The from domain is not verified."},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sender, _, _ := newResend(t, tc.status, `{"message":"The from domain is not verified."}`)

			err := sender.Send(t.Context(), ports.Message{To: "ada@example.com", Subject: "Hi", Text: "body"})

			resendErr, ok := errors.AsType[*mail.ResendError](err)
			require.True(t, ok, "error %v is not a *mail.ResendError", err)
			require.Equal(t, tc.status, resendErr.Status)
			require.Equal(t, tc.permanent, resendErr.Permanent())
			require.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestResendRejectsHeaderInjectionWithoutCalling(t *testing.T) {
	t.Parallel()

	sender, _, hits := newResend(t, http.StatusOK, `{"id":"email-1"}`)

	err := sender.Send(t.Context(), ports.Message{
		To: "ada@example.com", Subject: "hello\r\nBcc: evil@example.com", Text: "no",
	})
	require.Error(t, err)
	require.Zero(t, hits.Load())
}

func TestNewResendValidatesSettings(t *testing.T) {
	t.Parallel()

	_, err := mail.NewResend(" ", "blog@example.test", nil)
	require.ErrorContains(t, err, "api key")

	_, err = mail.NewResend("re_test", "not an address", nil)
	require.ErrorContains(t, err, "from address")
}
