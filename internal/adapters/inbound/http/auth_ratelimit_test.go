package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// denyLimiter refuses every request and records the buckets it was asked about.
type denyLimiter struct {
	mu   sync.Mutex
	keys []string
}

func (d *denyLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.keys = append(d.keys, key)

	return false, time.Second, nil
}

func TestAnonymousAuthEndpointsAreRateLimited(t *testing.T) {
	t.Parallel()

	limiter := &denyLimiter{}
	router, err := NewRouter(Dependencies{
		Logger: slog.New(slog.DiscardHandler), Auth: fakeAuthService{}, RateLimiter: limiter, LoginPerMinute: 5,
	})
	require.NoError(t, err)

	cases := []struct{ method, path, bucket string }{
		{nethttp.MethodPost, "/api/v1/auth/login", "auth.login:"},
		{nethttp.MethodPost, "/api/v1/auth/refresh", "auth.refresh:"},
		{nethttp.MethodPost, "/api/v1/auth/password/forgot", "auth.password:"},
		{nethttp.MethodGet, "/api/v1/auth/password/reset/some-token", "auth.password:"},
		{nethttp.MethodPost, "/api/v1/auth/password/reset", "auth.password:"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(`{}`)))
		require.Equal(t, nethttp.StatusTooManyRequests, w.Code, tc.path)
		require.NotEmpty(t, w.Header().Get("Retry-After"), tc.path)
		require.True(t, strings.HasPrefix(limiter.keys[len(limiter.keys)-1], tc.bucket), tc.path)
	}
}
