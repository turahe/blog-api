package ingestload

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPacesEventsAcrossTheIngestRoutes(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		routes    = map[string]int{}
		forwarded = map[string]bool{}
		agents    = map[string]bool{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	t.Cleanup(server.Close)

	result, err := Run(t.Context(), Config{BaseURL: server.URL + "/", Rate: 400, Duration: time.Second, Spread: 3, Sessions: 5})
	require.NoError(t, err)

	assert.Equal(t, int64(400), result.Planned)
	assert.Equal(t, result.Sent, result.Accepted)
	assert.Zero(t, result.Errors)
	require.NoError(t, result.Check(400, 0.9, time.Second))
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
