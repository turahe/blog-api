package bootstrap_test

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	httpadapter "github.com/turahe/blog-api/internal/adapters/inbound/http"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	"github.com/turahe/blog-api/internal/platform/ingestload"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

const (
	loadRate     = 500
	loadDuration = 10 * time.Second
	loadMaxP99   = 250 * time.Millisecond
)

// rawAnalyticsTables maps each raw event table to the ingest route that fills it.
var rawAnalyticsTables = map[string]string{
	"analytics_page_views": "page-view", "analytics_time_spent": "time-spent", "analytics_navigation": "navigation",
	"analytics_searches": "search", "analytics_search_clicks": "search-click",
}

// TestAnalyticsIngestSustainsPeakLoad drives the five ingest routes at the target peak
// through the real router and writer (default queue, batch, and flush settings) into
// PostgreSQL. Opt-in with TEST_ANALYTICS_LOAD=1; run without -race for honest latencies.
//
//nolint:paralleltest // latencies are only meaningful without other tests competing
func TestAnalyticsIngestSustainsPeakLoad(t *testing.T) {
	if os.Getenv("TEST_ANALYTICS_LOAD") != "1" {
		t.Skip("TEST_ANALYTICS_LOAD not set to 1; skipping analytics ingest load test")
	}

	var (
		writer  *analyticsservice.Writer
		dropped atomic.Int64
	)

	stack := newAuthStackWith(t, func(_ *gorm.DB, deps *httpadapter.Dependencies) {
		// Committed writes: batches from concurrent requests cannot share the test transaction.
		writer = analyticsservice.NewWriter(persistence.NewAnalyticsRepository(testDB), slog.New(slog.DiscardHandler),
			analyticsservice.WriterOptions{OnDrop: func(n int) { dropped.Add(int64(n)) }})
		deps.AnalyticsIngest = analyticsservice.NewIngest(writer, nil, uuidGen{}, system.Clock{})
	})

	server := httptest.NewServer(stack.router)
	t.Cleanup(server.Close)

	result, err := ingestload.Run(t.Context(), ingestload.Config{BaseURL: server.URL, Rate: loadRate, Duration: loadDuration})
	require.NoError(t, err)
	t.Cleanup(func() { deleteLoadEvents(t, result) })

	require.NoError(t, writer.Close(t.Context()))

	t.Logf("sent %d, accepted %d (%.1f events/s), p50 %s, p95 %s, p99 %s, max %s",
		result.Sent, result.Accepted, result.Rate(), result.P50, result.P95, result.P99, result.Max)

	require.NoError(t, result.Check(loadRate, 0.95, loadMaxP99))
	assert.Zero(t, dropped.Load(), "events dropped by the writer")

	for table, route := range rawAnalyticsTables {
		var stored int64
		require.NoError(t, testDB.Raw("SELECT count(*) FROM "+table+" WHERE session_id IN ?", result.Sessions).Scan(&stored).Error)

		accepted := result.Routes[route]
		require.Positive(t, accepted, route)

		if route == "time-spent" {
			// Heartbeats for one page view merge into its row.
			assert.LessOrEqual(t, stored, accepted, table)
			assert.Positive(t, stored, table)

			continue
		}

		assert.Equal(t, accepted, stored, "every accepted %s event is stored", route)
	}
}

func deleteLoadEvents(t *testing.T, result ingestload.Result) {
	t.Helper()

	if len(result.Sessions) == 0 {
		return
	}

	for table := range rawAnalyticsTables {
		if err := testDB.Exec("DELETE FROM "+table+" WHERE session_id IN ?", result.Sessions).Error; err != nil {
			t.Errorf("clean %s: %v", table, err)
		}
	}
}
