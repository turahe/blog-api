package handlers

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
)

const (
	codeRealtimeUnavailable = "analytics.realtime_unavailable"
	codeRealtimeLimit       = "analytics.realtime_limit"
)

type analyticsLiveBoard interface {
	Snapshot(cursor uint64) (analyticsdomain.LiveSnapshot, uint64)
}

// adminAnalyticsRealtimeHandler godoc
//
//	@Summary		Stream live analytics
//	@Description	Server-sent events. The stream opens with `retry: 5000` and `event: stream.opened`, then every 5 seconds (and once right away) sends `event: realtime.page_view` and `event: realtime.search` for events accepted since the previous frame (at most 20 of each, the latest when busier; the first frame replays up to 20 recent ones), followed by `event: realtime.summary`: sessions active in the last 5 minutes, views and searches per minute for the last 30 minutes, and the top 10 pages of those 30 minutes. The summary doubles as the heartbeat. `event: stream.closed` (code shutdown) precedes a server shutdown. Counts cover every API replica through the message broker but start empty when a replica starts. Page views carry path, country, and device; searches the normalised query and result count; nothing identifies a visitor. Returns 429 analytics.realtime_limit (with Retry-After) past the per-user stream limit and 503 analytics.realtime_unavailable without a message broker.
//	@Tags			admin
//	@Produce		text/event-stream
//	@Success		200	{string}	string	"event stream"
//	@Failure		401	{object}	responses.Envelope
//	@Failure		403	{object}	responses.Envelope
//	@Failure		429	{object}	responses.Envelope
//	@Failure		503	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/analytics/realtime/stream [get]
func adminAnalyticsRealtimeHandler(board analyticsLiveBoard, hub notificationStreamHub, refresh time.Duration) gin.HandlerFunc {
	if refresh <= 0 {
		refresh = analyticsdomain.LiveRefresh
	}

	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		if board == nil || hub == nil {
			responses.Failure(c, nethttp.StatusServiceUnavailable, codeRealtimeUnavailable, "Live analytics are not available")
			return
		}

		conn, err := hub.Register(userID)

		switch {
		case errors.Is(err, realtime.ErrTooManyConnections):
			c.Header("Retry-After", streamLimitRetryAfter)
			responses.Failure(c, nethttp.StatusTooManyRequests, codeRealtimeLimit, "Too many open live analytics streams")

			return
		case err != nil:
			responses.Failure(c, nethttp.StatusServiceUnavailable, codeRealtimeUnavailable, "Live analytics are not available")
			return
		}

		defer hub.Unregister(conn)

		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Status(nethttp.StatusOK)

		streamLive(c, conn, board, refresh)
	}
}

// streamLive writes frames until the client leaves, the server shuts down, or a write fails.
func streamLive(c *gin.Context, conn *realtime.Conn, board analyticsLiveBoard, refresh time.Duration) {
	w := c.Writer

	if _, err := fmt.Fprintf(w, "retry: %d\n\n", streamRetryMS); err != nil {
		return
	}

	opened := gin.H{"streamId": uuid.New(), "serverTs": time.Now().UTC(), "refreshSeconds": int(refresh / time.Second)}
	if writeFrame(w, "stream.opened", uuid.NewString(), opened) != nil {
		return
	}

	var cursor uint64

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()

	for {
		var snapshot analyticsdomain.LiveSnapshot

		snapshot, cursor = board.Snapshot(cursor)
		if writeLive(w, snapshot, cursor) != nil {
			return
		}

		w.Flush()

		select {
		case <-c.Request.Context().Done():
			return
		case <-conn.Done():
			writeShutdown(w)
			return
		case <-ticker.C:
		}
	}
}

func writeLive(w gin.ResponseWriter, snapshot analyticsdomain.LiveSnapshot, cursor uint64) error {
	for _, e := range snapshot.RecentViews {
		if err := writeFrame(w, "realtime.page_view", "", responses.AnalyticsLivePageView(e)); err != nil {
			return err
		}
	}

	for _, e := range snapshot.RecentSearches {
		if err := writeFrame(w, "realtime.search", "", responses.AnalyticsLiveSearch(e)); err != nil {
			return err
		}
	}

	return writeFrame(w, "realtime.summary", strconv.FormatUint(cursor, 10), responses.AnalyticsLiveSummary(snapshot))
}
