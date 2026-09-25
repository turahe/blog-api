package handlers

import (
	"bufio"
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func streamServer(t *testing.T, hub notificationStreamHub, user uuid.UUID) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/stream", func(c *gin.Context) { c.Set(middleware.ContextUserIDKey, user) },
		meNotificationsStreamHandler(hub, time.Second))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server
}

func streamRequest(t *testing.T, server *httptest.Server) *nethttp.Request {
	t.Helper()

	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, server.URL+"/stream", nil)
	require.NoError(t, err)

	return req
}

// readFrame returns the next SSE frame as its field lines, skipping blank separators.
func readFrame(t *testing.T, r *bufio.Reader) []string {
	t.Helper()

	var lines []string

	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err)

		line = strings.TrimRight(line, "\n")
		if line == "" {
			if len(lines) > 0 {
				return lines
			}

			continue
		}

		lines = append(lines, line)
	}
}

func TestNotificationStreamLifecycle(t *testing.T) {
	t.Parallel()

	hub := realtime.NewHub(1, 8)
	user := uuid.New()
	server := streamServer(t, hub, user)

	ctx, cancel := context.WithCancel(t.Context())

	resp, err := server.Client().Do(streamRequest(t, server).WithContext(ctx))
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, nethttp.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
	require.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))

	body := bufio.NewReader(resp.Body)
	require.Equal(t, []string{"retry: 5000"}, readFrame(t, body))

	opened := readFrame(t, body)
	require.Equal(t, "event: stream.opened", opened[0])
	require.Contains(t, opened[2], user.String())

	second, err := server.Client().Do(streamRequest(t, server))
	require.NoError(t, err)
	require.NoError(t, second.Body.Close())
	require.Equal(t, nethttp.StatusTooManyRequests, second.StatusCode, "the per-user limit applies")
	require.Equal(t, "60", second.Header.Get("Retry-After"))

	n := notificationdomain.Notification{UUID: uuid.New(), UserUUID: user, Type: "comment.reply", Title: "hi"}
	hub.Deliver(notificationdomain.Notification{UUID: uuid.New(), UserUUID: uuid.New(), Type: "other"})
	hub.Deliver(n)

	frame := readFrame(t, body)
	for frame[0] == "event: ping" {
		frame = readFrame(t, body)
	}

	require.Equal(t, "event: notification.created", frame[0])
	require.Equal(t, "id: "+n.UUID.String(), frame[1])
	require.Contains(t, frame[2], `"type":"comment.reply"`)
	require.NotContains(t, frame[2], "is_read")

	require.Equal(t, "event: ping", readFrame(t, body)[0], "idle streams are pinged")

	cancel()
	require.Eventually(t, func() bool { return hub.Connections() == 0 }, 5*time.Second, 10*time.Millisecond,
		"a client disconnect releases the stream")
}

func TestNotificationStreamShutdown(t *testing.T) {
	t.Parallel()

	hub := realtime.NewHub(1, 8)
	server := streamServer(t, hub, uuid.New())

	resp, err := server.Client().Do(streamRequest(t, server))
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	body := bufio.NewReader(resp.Body)
	readFrame(t, body)
	readFrame(t, body)

	hub.Shutdown()

	frame := readFrame(t, body)
	for frame[0] == "event: ping" {
		frame = readFrame(t, body)
	}

	require.Equal(t, "event: stream.closed", frame[0])
	require.Contains(t, frame[2], `"code":"shutdown"`)
}

func TestNotificationStreamUnavailableWithoutHub(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	w, body := runProfile(t, meNotificationsStreamHandler(nil, 0), profileRequest{
		method: nethttp.MethodGet, target: "/me/notifications/stream", user: &user,
	})

	require.Equal(t, nethttp.StatusServiceUnavailable, w.Code)
	require.Equal(t, codeStreamUnavailable, errorCode(body))
}
