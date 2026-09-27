package ingestload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handlerTransport serves requests in-process so the run stays inside a synctest bubble:
// a real socket would let wall-clock load decide how many events are skipped.
type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	defer func() { _ = r.Body.Close() }()

	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)

	return rec.Result(), nil
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	_ = r.Body.Close()

	return nil, f.err
}

func TestRateWithoutElapsedTime(t *testing.T) {
	t.Parallel()

	assert.Zero(t, Result{Accepted: 10}.Rate())
	assert.InDelta(t, 5, Result{Accepted: 10, Elapsed: 2 * time.Second}.Rate(), 1e-9)
}

func TestRunPacesEventsAcrossTheIngestRoutes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			mu        sync.Mutex
			routes    = map[string]int{}
			forwarded = map[string]bool{}
			agents    = map[string]bool{}
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["session_id"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			mu.Lock()
			routes[strings.TrimPrefix(r.URL.Path, "/api/v1/analytics/ingest/")]++
			forwarded[r.Header.Get("X-Forwarded-For")] = true
			agents[r.Header.Get("User-Agent")] = true
			mu.Unlock()

			w.WriteHeader(http.StatusAccepted)
		})

		result, err := Run(t.Context(), Config{
			BaseURL: "http://ingest.test/", Rate: 400, Duration: time.Second, Spread: 3, Sessions: 5,
			Client: &http.Client{Transport: handlerTransport{handler: handler}},
		})
		require.NoError(t, err)

		assert.Equal(t, int64(400), result.Planned)
		assert.Equal(t, int64(400), result.Sent)
		assert.Equal(t, result.Sent, result.Accepted)
		assert.Zero(t, result.Skipped)
		assert.Zero(t, result.Errors)
		assert.Equal(t, time.Second, result.Elapsed)
		require.NoError(t, result.Check(400, 1, 0))
		assert.Len(t, result.Sessions, 5)
		assert.True(t, strings.HasPrefix(result.PathPrefix, "/loadtest/"))
		assert.LessOrEqual(t, result.P50, result.P99)

		mu.Lock()
		defer mu.Unlock()

		for route, n := range routes {
			assert.Equal(t, int64(n), result.Routes[route], route)
		}

		assert.ElementsMatch(t, []string{"page-view", "time-spent", "navigation", "search", "search-click"}, keysOf(routes))
		assert.Greater(t, routes["page-view"], routes["search-click"])
		assert.Len(t, forwarded, 3)
		assert.Len(t, agents, 1)
		assert.NotContains(t, agents, "")
	})
}

func TestCheckReportsEveryMiss(t *testing.T) {
	t.Parallel()

	result := Result{
		Accepted: 50, Statuses: map[int]int64{http.StatusAccepted: 50, http.StatusTooManyRequests: 3},
		Errors: 1, Skipped: 2, Elapsed: time.Second, P99: 2 * time.Second,
	}

	err := result.Check(100, 0.95, time.Second)
	require.Error(t, err)

	for _, want := range []string{"status 429", "1 requests failed", "2 events skipped", "accepted 50.0 events/s", "p99 latency"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		result, err := Run(ctx, Config{
			BaseURL: "http://ingest.test", Rate: 100, Duration: time.Second,
			Client: &http.Client{Transport: failingTransport{err: errors.New("unused")}},
		})
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, result.Planned)
		assert.Zero(t, result.Sent)
	})
}

func TestRunSkipsEventsWhenWorkersFallBehind(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusAccepted)
		})

		result, err := Run(t.Context(), Config{
			BaseURL: "http://ingest.test", Rate: 100, Duration: time.Second, Workers: 1,
			Client: &http.Client{Transport: handlerTransport{handler: slow}},
		})
		require.NoError(t, err)

		assert.Equal(t, int64(100), result.Planned)
		assert.Positive(t, result.Skipped)
		assert.Equal(t, result.Planned, result.Sent+result.Skipped)
		assert.Equal(t, result.Sent, result.Accepted)
		require.ErrorContains(t, result.Check(100, 0, 0), "events skipped")
	})
}

func TestRunCountsTransportFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "transport error", baseURL: "http://ingest.test"},
		{name: "invalid base url", baseURL: "http://bad host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				result, err := Run(t.Context(), Config{
					BaseURL: tt.baseURL, Rate: 100, Duration: 100 * time.Millisecond,
					Client: &http.Client{Transport: failingTransport{err: errors.New("connection refused")}},
				})
				require.NoError(t, err)

				assert.Positive(t, result.Sent)
				assert.Equal(t, result.Sent, result.Errors)
				assert.Zero(t, result.Accepted)
				assert.Empty(t, result.Statuses)
				assert.Zero(t, result.Max, "no latency is recorded for failed requests")
				require.ErrorContains(t, result.Check(100, 0, 0), "requests failed")
			})
		})
	}
}

func TestWithDefaults(t *testing.T) {
	t.Parallel()

	got := withDefaults(Config{BaseURL: "http://api.test//", Rate: 1000})

	assert.Equal(t, 100, got.Workers)
	assert.Equal(t, 200, got.Sessions)
	assert.True(t, strings.HasPrefix(got.PathPrefix, "/loadtest/"))
	assert.Equal(t, "http://api.test", got.BaseURL)
	require.NotNil(t, got.Client)
	assert.Equal(t, 10*time.Second, got.Client.Timeout)

	assert.Equal(t, 8, withDefaults(Config{Rate: 10}).Workers, "at least eight workers")
}

func TestDroppedEventsReadsTheCounter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "# HELP x\nblog_analytics_events_dropped_total_other 9\nblog_analytics_events_dropped_total 4\n")
	}))
	t.Cleanup(server.Close)

	dropped, err := DroppedEvents(t.Context(), server.Client(), server.URL)
	require.NoError(t, err)
	assert.InDelta(t, 4, dropped, 0)
}

func TestDroppedEventsFailures(t *testing.T) {
	t.Parallel()

	serve := func(status int, body string) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)

		return server.URL
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "invalid url", url: "://bad", wantErr: "missing protocol scheme"},
		{name: "unreachable", url: closed.URL, wantErr: "connect"},
		{name: "bad status", url: serve(http.StatusServiceUnavailable, ""), wantErr: "metrics: status 503"},
		{name: "oversized line", url: serve(http.StatusOK, strings.Repeat("x", 70_000)), wantErr: "token too long"},
		{name: "metric missing", url: serve(http.StatusOK, "other_metric 1\n"), wantErr: "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dropped, err := DroppedEvents(t.Context(), http.DefaultClient, tt.url)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, dropped)
		})
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	t.Parallel()

	_, err := Run(t.Context(), Config{BaseURL: "http://127.0.0.1:1", Rate: 0, Duration: time.Second})
	require.Error(t, err)
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
