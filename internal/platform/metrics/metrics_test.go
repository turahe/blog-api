package metrics

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noDriver struct{}

func (noDriver) Open(string) (driver.Conn, error) { return nil, errors.New("no connections") }

type noConnector struct{}

func (noConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("no connections")
}

func (noConnector) Driver() driver.Driver { return noDriver{} }

func scrape(t *testing.T, handler nethttp.Handler, path string) (int, string) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, server.URL+path, nil)
	require.NoError(t, err)

	resp, err := nethttp.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

func TestNewRegistersDBStats(t *testing.T) {
	t.Parallel()

	db := sql.OpenDB(noConnector{})
	t.Cleanup(func() { _ = db.Close() })

	status, body := scrape(t, New(db, "v1").Handler(), "")

	require.Equal(t, nethttp.StatusOK, status)
	assert.Contains(t, body, `go_sql_open_connections{db_name="blog"} 0`)
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1")
	extra := prometheus.NewCounter(prometheus.CounterOpts{Name: "extra_total", Help: "Extra."})
	m.Registry().MustRegister(extra)
	extra.Inc()

	_, body := scrape(t, m.Handler(), "")

	assert.Contains(t, body, "extra_total 1")
}

func TestObserveHTTPIsExposed(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1.2.3")
	done := m.StartRequest()
	require.InDelta(t, 1, testutil.ToFloat64(m.inflight), 0)
	done()
	require.InDelta(t, 0, testutil.ToFloat64(m.inflight), 0)

	m.ObserveHTTP(nethttp.MethodPost, "/api/v1/auth/login", nethttp.StatusUnauthorized, 20*time.Millisecond)
	m.ObserveHTTP(nethttp.MethodPost, "/api/v1/auth/login", nethttp.StatusUnauthorized, 30*time.Millisecond)

	require.InDelta(t, 2, testutil.ToFloat64(m.requests.WithLabelValues("POST", "/api/v1/auth/login", "401")), 0)

	status, body := scrape(t, m.NewServer("").Handler, "/metrics")

	require.Equal(t, nethttp.StatusOK, status)
	require.Contains(t, body, `blog_http_requests_total{method="POST",route="/api/v1/auth/login",status="401"} 2`)
	require.Contains(t, body, `blog_build_info{version="v1.2.3"} 1`)
	require.Contains(t, body, "go_goroutines")
}

func TestDroppedCounters(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1")
	m.AuditDropped(3)
	m.AuditDropped(2)
	m.AnalyticsDropped(7)

	assert.InDelta(t, 5, testutil.ToFloat64(m.dropped), 0)
	assert.InDelta(t, 7, testutil.ToFloat64(m.analyticsDropped), 0)
}

func TestOutboxPublished(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1")
	m.OutboxPublished()
	m.OutboxPublished()

	assert.InDelta(t, 2, testutil.ToFloat64(m.outboxPublished), 0)
}

func TestOutboxPublishFailed(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1")
	m.OutboxPublishFailed(true)
	m.OutboxPublishFailed(false)
	m.OutboxPublishFailed(false)

	assert.InDelta(t, 1, testutil.ToFloat64(m.outboxFailed.WithLabelValues("true")), 0)
	assert.InDelta(t, 2, testutil.ToFloat64(m.outboxFailed.WithLabelValues("false")), 0)
}

func TestOutboxBacklog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	past := now.Add(-90 * time.Second)
	future := now.Add(time.Minute)

	tests := []struct {
		name    string
		pending int64
		failed  int64
		oldest  *time.Time
		wantLag float64
	}{
		{name: "empty backlog has no lag", pending: 0, failed: 0, oldest: nil, wantLag: 0},
		{name: "lag is age of oldest pending", pending: 4, failed: 2, oldest: &past, wantLag: 90},
		{name: "clock skew clamps lag to zero", pending: 1, failed: 0, oldest: &future, wantLag: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New(nil, "v1")
			m.OutboxBacklog(tt.pending, tt.failed, tt.oldest, now)

			assert.InDelta(t, float64(tt.pending), testutil.ToFloat64(m.outboxPending), 0)
			assert.InDelta(t, float64(tt.failed), testutil.ToFloat64(m.outboxParked), 0)
			assert.InDelta(t, tt.wantLag, testutil.ToFloat64(m.outboxLag), 0)
		})
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	m := New(nil, "v1")
	probe := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		_, _ = io.WriteString(w, "ready")
	})

	server := m.NewServer(":9100", Route{Pattern: "GET /readyz", Handler: probe})

	assert.Equal(t, ":9100", server.Addr)
	assert.Equal(t, 5*time.Second, server.ReadHeaderTimeout)
	assert.Equal(t, 10*time.Second, server.ReadTimeout)
	assert.Equal(t, 30*time.Second, server.WriteTimeout)

	status, body := scrape(t, server.Handler, "/readyz")
	assert.Equal(t, nethttp.StatusOK, status)
	assert.Equal(t, "ready", body)

	status, _ = scrape(t, server.Handler, "/unknown")
	assert.Equal(t, nethttp.StatusNotFound, status)
}
