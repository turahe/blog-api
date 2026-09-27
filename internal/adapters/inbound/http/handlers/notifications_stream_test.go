package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/realtime"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func streamServer(t *testing.T, hub notificationStreamHub, user uuid.UUID) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/stream", func(c *gin.Context) { c.Set(middleware.ContextUserIDKey, user) },
		meNotificationsStreamHandler(hub, time.Second, nil))

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
	require.NotContains(t, frame[2], "isRead")

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

type toggleVerifier struct {
	active atomic.Bool
	calls  atomic.Int32
}

func (v *toggleVerifier) Verify(context.Context, string, uuid.UUID, uuid.UUID) error {
	v.calls.Add(1)

	if v.active.Load() {
		return nil
	}

	return impdomain.ErrEnded
}

func TestNotificationStreamClosesWhenImpersonationEnds(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	hub := realtime.NewHub(1, 8)
	target, actor := uuid.New(), uuid.New()
	verifier := &toggleVerifier{}
	verifier.active.Store(true)

	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		c.Set(middleware.ContextUserIDKey, target)
		c.Set(middleware.ContextClaimsKey, authdomain.AccessClaims{Subject: target, Actor: &actor, SessionID: uuid.NewString()})
	}, meNotificationsStreamHandler(hub, time.Hour, verifier))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	resp, err := server.Client().Do(streamRequest(t, server))
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	body := bufio.NewReader(resp.Body)
	readFrame(t, body)
	readFrame(t, body)

	hub.Deliver(notificationdomain.Notification{UUID: uuid.New(), UserUUID: target, Type: "first"})
	require.Equal(t, "event: notification.created", readFrame(t, body)[0], "delivered while the session is active")

	verifier.active.Store(false)
	hub.Deliver(notificationdomain.Notification{UUID: uuid.New(), UserUUID: target, Type: "second"})

	closed := readFrame(t, body)
	require.Equal(t, "event: stream.closed", closed[0])
	require.Contains(t, closed[2], `"code":"impersonation_ended"`)
	require.NotContains(t, closed[2], "second")
	require.Eventually(t, func() bool { return hub.Connections() == 0 }, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 2, verifier.calls.Load(), "checked before every frame")
}

func TestNotificationStreamUnavailableWithoutHub(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	w, body := runProfile(t, meNotificationsStreamHandler(nil, 0, nil), profileRequest{
		method: nethttp.MethodGet, target: "/me/notifications/stream", user: &user,
	})

	require.Equal(t, nethttp.StatusServiceUnavailable, w.Code)
	require.Equal(t, codeStreamUnavailable, errorCode(body))
}

func TestNotificationStreamRejectsBeforeStreaming(t *testing.T) {
	t.Parallel()

	stopped := realtime.NewHub(1, 1)
	stopped.Shutdown()

	tests := []struct {
		name   string
		hub    notificationStreamHub
		user   *uuid.UUID
		status int
		code   string
	}{
		{name: "needs sign-in", hub: realtime.NewHub(1, 1), status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "hub shut down", hub: stopped, user: &testUserID, status: nethttp.StatusServiceUnavailable, code: codeStreamUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, meNotificationsStreamHandler(tc.hub, 0, nil), profileRequest{
				method: nethttp.MethodGet, target: "/", user: tc.user,
			})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

// failingWriter fails the failAt-th write (1-based) and every write after it.
type failingWriter struct {
	gin.ResponseWriter

	failAt int
	writes int
}

var errWriteFailed = errors.New("client went away")

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= w.failAt {
		return 0, errWriteFailed
	}

	return w.ResponseWriter.Write(p)
}

func (w *failingWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// streamContext returns a context for a signed-in client whose writes fail from failAt on.
func streamContext(t *testing.T, failAt int) (*gin.Context, *failingWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/", nil)
	c.Set(middleware.ContextUserIDKey, testUserID)

	w := &failingWriter{ResponseWriter: c.Writer, failAt: failAt}
	c.Writer = w

	return c, w
}

func TestStreamNotificationsStopsOnWriteFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		failAt int
	}{
		{name: "retry hint", failAt: 1},
		{name: "opened frame", failAt: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hub := realtime.NewHub(1, 1)
			conn, err := hub.Register(testUserID)
			require.NoError(t, err)

			c, w := streamContext(t, tc.failAt)
			streamNotifications(c, conn, testUserID, time.Hour, nil)
			require.Equal(t, tc.failAt, w.writes, "no write after the failure")
		})
	}
}

func TestWriteDroppedReportsOverflow(t *testing.T) {
	t.Parallel()

	hub := realtime.NewHub(1, 1)
	conn, err := hub.Register(testUserID)
	require.NoError(t, err)

	for range 3 {
		hub.Deliver(notificationdomain.Notification{UUID: uuid.New(), UserUUID: testUserID})
	}

	var out strings.Builder
	require.NoError(t, writeDropped(&out, conn))
	require.Contains(t, out.String(), "event: error\n")
	require.Contains(t, out.String(), `"code":"fanout.buffer_full","droppedCount":2`)
}

func TestWriteFrameRejectsUnencodableData(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, writeFrame(&out, "ping", "", make(chan int)), &unsupported)
	require.Empty(t, out.String())
}
