package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

const (
	streamRetryMS         = 5000
	streamShutdownRetryMS = 15000
	streamLimitRetryAfter = "60"
	defaultStreamPing     = 15 * time.Second

	codeStreamUnavailable = "notifications.stream_unavailable"
	codeStreamLimit       = "notifications.stream_limit"
)

type notificationStreamHub interface {
	Register(userID uuid.UUID) (*realtime.Conn, error)
	Unregister(conn *realtime.Conn)
}

// meNotificationsStreamHandler godoc
//
//	@Summary		Stream my notifications
//	@Description	Server-sent events. The stream opens with `retry: 5000` and `event: stream.opened`, sends `event: notification.created` for each new notice (id is the notification id), `event: ping` when idle, `event: error` with code fanout.buffer_full when events were dropped for a slow client, and `event: stream.closed` before a server shutdown (code shutdown) or once the impersonation session behind the token ends (code impersonation_ended). There is no replay: after reconnecting, call GET /api/v1/me/notifications to fill gaps. Returns 429 notifications.stream_limit (with Retry-After) past the per-user limit and 503 notifications.stream_unavailable without a message broker.
//	@Tags			me
//	@Produce		text/event-stream
//	@Success		200	{string}	string	"event stream"
//	@Failure		401	{object}	responses.Envelope
//	@Failure		429	{object}	responses.Envelope
//	@Failure		503	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/notifications/stream [get]
func meNotificationsStreamHandler(hub notificationStreamHub, ping time.Duration, sessions middleware.ImpersonationVerifier) gin.HandlerFunc {
	if ping < time.Second {
		ping = defaultStreamPing
	}

	return func(c *gin.Context) {
		userID, ok := currentUser(c)
		if !ok {
			return
		}

		if hub == nil {
			failNotification(c, nethttp.StatusServiceUnavailable, codeStreamUnavailable, "Live notifications are not available")
			return
		}

		conn, err := hub.Register(userID)

		switch {
		case errors.Is(err, realtime.ErrTooManyConnections):
			c.Header("Retry-After", streamLimitRetryAfter)
			failNotification(c, nethttp.StatusTooManyRequests, codeStreamLimit, "Too many open notification streams")

			return
		case err != nil:
			failNotification(c, nethttp.StatusServiceUnavailable, codeStreamUnavailable, "Live notifications are not available")
			return
		}

		defer hub.Unregister(conn)

		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Status(nethttp.StatusOK)

		streamNotifications(c, conn, userID, ping, impersonationAlive(c, sessions, userID))
	}
}

// impersonationAlive returns nil for a normal token. For an impersonation token it returns a
// check that the session is still active: the token was verified only when the stream opened,
// and a stopped or expired session must stop receiving the target's notices.
func impersonationAlive(c *gin.Context, sessions middleware.ImpersonationVerifier, userID uuid.UUID) func() bool {
	imp, ok := middleware.CurrentImpersonation(c)
	if !ok {
		return nil
	}

	return func() bool {
		return sessions != nil && sessions.Verify(c.Request.Context(), imp.SessionID.String(), imp.ActorID, userID) == nil
	}
}

// checkSession writes stream.closed and returns errImpersonationEnded once alive reports the
// impersonation session over.
func checkSession(w io.Writer, alive func() bool) error {
	if alive == nil || alive() {
		return nil
	}

	_ = writeFrame(w, "stream.closed", uuid.NewString(), gin.H{"code": "impersonation_ended"})

	return errImpersonationEnded
}

var errImpersonationEnded = errors.New("impersonation session ended")

// streamNotifications writes frames until the client leaves, the hub shuts down, an
// impersonation session ends, or a write fails.
func streamNotifications(c *gin.Context, conn *realtime.Conn, userID uuid.UUID, ping time.Duration, alive func() bool) {
	w := c.Writer

	opened := gin.H{
		"stream_id": uuid.New(), "user_id": userID, "server_ts": time.Now().UTC(),
		"retry_ms": streamRetryMS, "channels": []string{"default"}, "replay_applied": false, "replay_count": 0,
	}
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", streamRetryMS); err != nil {
		return
	}

	if writeFrame(w, "stream.opened", uuid.NewString(), opened) != nil {
		return
	}

	w.Flush()

	ticker := time.NewTicker(ping)
	defer ticker.Stop()

	for {
		var err error

		select {
		case <-c.Request.Context().Done():
			return
		case <-conn.Done():
			writeShutdown(w)
			return
		case n := <-conn.Events():
			if err = checkSession(w, alive); err == nil {
				err = writeDropped(w, conn)
			}

			if err == nil {
				err = writeFrame(w, "notification.created", n.UUID.String(), streamNotification(n))
			}
		case now := <-ticker.C:
			if err = checkSession(w, alive); err == nil {
				err = writeDropped(w, conn)
			}

			if err == nil {
				err = writeFrame(w, "ping", "", gin.H{"ts": now.UTC()})
			}
		}

		if err != nil {
			w.Flush()
			return
		}

		w.Flush()
	}
}

// writeShutdown tells the client the server is stopping and when to reconnect.
func writeShutdown(w gin.ResponseWriter) {
	_ = writeFrame(w, "stream.closed", uuid.NewString(), gin.H{"code": "shutdown", "retry_ms": streamShutdownRetryMS})
	w.Flush()
}

func writeDropped(w io.Writer, conn *realtime.Conn) error {
	dropped := conn.TakeDropped()
	if dropped == 0 {
		return nil
	}

	return writeFrame(w, "error", uuid.NewString(), gin.H{"code": "fanout.buffer_full", "dropped_count": dropped})
}

func writeFrame(w io.Writer, event, id string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	frame := "event: " + event + "\n"
	if id != "" {
		frame += "id: " + id + "\n"
	}

	_, err = io.WriteString(w, frame+"data: "+string(payload)+"\n\n")

	return err
}

func streamNotification(n notificationdomain.Notification) gin.H {
	item := responses.Notification(n)
	delete(item, "is_read")
	delete(item, "read_at")

	return item
}
