package captcha

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func siteverify(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.ParseForm() != nil || r.PostForm.Get("secret") != "secret-key" ||
			r.PostForm.Get("remoteip") != "203.0.113.9" || r.PostForm.Get("response") == "" {
			w.WriteHeader(http.StatusTeapot)
			return
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

func TestTurnstileAcceptsValidToken(t *testing.T) {
	t.Parallel()

	srv, _ := siteverify(t, http.StatusOK, `{"success":true}`)

	ok, err := NewTurnstile("secret-key", srv.URL, nil).Verify(t.Context(), "token", "203.0.113.9")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestTurnstileRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	srv, _ := siteverify(t, http.StatusOK, `{"success":false,"error-codes":["invalid-input-response"]}`)

	ok, err := NewTurnstile("secret-key", srv.URL, nil).Verify(t.Context(), "token", "203.0.113.9")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTurnstileSkipsProviderForMissingOrOversizedToken(t *testing.T) {
	t.Parallel()

	srv, calls := siteverify(t, http.StatusOK, `{"success":true}`)
	v := NewTurnstile("secret-key", srv.URL, nil)

	for _, token := range []string{"", "   ", strings.Repeat("x", maxTurnstileToken+1)} {
		ok, err := v.Verify(t.Context(), token, "203.0.113.9")
		require.NoError(t, err)
		require.False(t, ok)
	}

	require.Zero(t, calls.Load())
}

func TestTurnstileReportsProviderFaults(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"bad secret": {http.StatusOK, `{"success":false,"error-codes":["invalid-input-secret"]}`},
		"5xx":        {http.StatusBadGateway, `oops`},
		"not json":   {http.StatusOK, `<html>`},
	} {
		srv, _ := siteverify(t, tc.status, tc.body)

		ok, err := NewTurnstile("secret-key", srv.URL, nil).Verify(t.Context(), "token", "203.0.113.9")
		require.Error(t, err, name)
		require.False(t, ok, name)
	}
}
