package handlers

import (
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
)

type fakeLiveBoard struct{ cursors []uint64 }

func (f *fakeLiveBoard) Snapshot(cursor uint64) (analyticsdomain.LiveSnapshot, uint64) {
	f.cursors = append(f.cursors, cursor)
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	return analyticsdomain.LiveSnapshot{
		At: at, ActiveSessions: 2,
		Series:   []analyticsdomain.LiveMinute{{Start: at, Views: 3, Searches: 1}},
		TopPages: []analyticsdomain.PathCount{{Path: "/a", Count: 3}},
		RecentViews: []analyticsdomain.LiveEvent{{
			Kind: analyticsdomain.KindPageView, At: at, Session: uuid.New(), Path: "/a", Country: "DE",
			Device: analyticsdomain.DeviceMobile,
		}},
		RecentSearches: []analyticsdomain.LiveEvent{{Kind: analyticsdomain.KindSearch, At: at, Session: uuid.New(), Query: "go", Results: 4}},
	}, 7
}

// runClosedStream runs handler with a request whose client has already gone, so the stream
// writes its first frames and returns.
func runClosedStream(t *testing.T, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(ctx, nethttp.MethodGet, "/", nil)
	c.Set(middleware.ContextUserIDKey, testUserID)
	handler(c)

	return w
}

func TestAdminAnalyticsRealtimeStreamsEventsAndSummary(t *testing.T) {
	t.Parallel()

	board := &fakeLiveBoard{}
	hub := realtime.NewHub(1, 1)
	w := runClosedStream(t, adminAnalyticsRealtimeHandler(board, hub, time.Second))

	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

	body := w.Body.String()
	assert.Contains(t, body, "retry: 5000")
	assert.Contains(t, body, "event: stream.opened")
	assert.Contains(t, body, "event: realtime.page_view\ndata: {\"country\":\"DE\",\"device\":\"mobile\",\"path\":\"/a\"")
	assert.Contains(t, body, "event: realtime.search\ndata: {\"query\":\"go\",\"resultCount\":4")
	assert.Contains(t, body, "event: realtime.summary\nid: 7\n")
	assert.Contains(t, body, `"activeSessions":2`)
	assert.Contains(t, body, `"topPages":[{"path":"/a","views":3}]`)
	assert.NotContains(t, body, `"session`, "no session ids reach the client")
	assert.Equal(t, []uint64{0}, board.cursors)
	assert.Zero(t, hub.Connections(), "the stream slot is released")
}

func TestAdminAnalyticsRealtimeIsUnavailableWithoutABroker(t *testing.T) {
	t.Parallel()

	w, body := runProfile(t, adminAnalyticsRealtimeHandler(nil, nil, 0), profileRequest{
		method: nethttp.MethodGet, target: "/", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusServiceUnavailable, w.Code)
	assert.Equal(t, codeRealtimeUnavailable, errorCode(body))
}

func TestAdminAnalyticsRealtimeLimitsStreamsPerUser(t *testing.T) {
	t.Parallel()

	hub := realtime.NewHub(1, 1)
	_, err := hub.Register(testUserID)
	require.NoError(t, err)

	w, body := runProfile(t, adminAnalyticsRealtimeHandler(&fakeLiveBoard{}, hub, 0), profileRequest{
		method: nethttp.MethodGet, target: "/", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusTooManyRequests, w.Code)
	assert.Equal(t, codeRealtimeLimit, errorCode(body))
	assert.Equal(t, streamLimitRetryAfter, w.Header().Get("Retry-After"))
}

func TestAdminAnalyticsRealtimeClosesOnShutdown(t *testing.T) {
	t.Parallel()

	hub := realtime.NewHub(1, 1)
	hub.Shutdown()

	w, body := runProfile(t, adminAnalyticsRealtimeHandler(&fakeLiveBoard{}, hub, 0), profileRequest{
		method: nethttp.MethodGet, target: "/", user: &testUserID,
	})
	require.Equal(t, nethttp.StatusServiceUnavailable, w.Code, "no new streams after shutdown")
	assert.Equal(t, codeRealtimeUnavailable, errorCode(body))
}
