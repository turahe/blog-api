package metrics

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestObserveHTTPIsExposed(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1.2.3")
	done := m.StartRequest()
	require.InDelta(t, 1, testutil.ToFloat64(m.inflight), 0)
	done()

	m.ObserveHTTP(nethttp.MethodPost, "/api/v1/auth/login", nethttp.StatusUnauthorized, 20*time.Millisecond)
	m.ObserveHTTP(nethttp.MethodPost, "/api/v1/auth/login", nethttp.StatusUnauthorized, 30*time.Millisecond)

	require.InDelta(t, 2, testutil.ToFloat64(m.requests.WithLabelValues("POST", "/api/v1/auth/login", "401")), 0)

	server := httptest.NewServer(m.NewServer("").Handler)
	t.Cleanup(server.Close)

	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, server.URL+"/metrics", nil)
	require.NoError(t, err)

	resp, err := nethttp.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, nethttp.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), `blog_http_requests_total{method="POST",route="/api/v1/auth/login",status="401"} 2`)
	require.Contains(t, string(body), `blog_build_info{version="v1.2.3"} 1`)
	require.Contains(t, string(body), "go_goroutines")
}
