package sentry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/logging"
)

type fakeTransport struct {
	mu     sync.Mutex
	events []*sentrygo.Event
}

func (f *fakeTransport) Flush(time.Duration) bool              { return true }
func (f *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (f *fakeTransport) Configure(sentrygo.ClientOptions)      {}
func (f *fakeTransport) Close()                                {}

func (f *fakeTransport) SendEvent(event *sentrygo.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.events = append(f.events, event)
}

func (f *fakeTransport) Events() []*sentrygo.Event {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*sentrygo.Event(nil), f.events...)
}

// enable binds a global Sentry client backed by a fake transport for one test.
func enable(t *testing.T) *fakeTransport {
	t.Helper()

	transport := &fakeTransport{}
	flush, err := initWithTransport(config.Config{
		SentryDSN:         "https://public@example.com/1",
		SentryEnvironment: "test",
	}, "v1.2.3", transport)
	require.NoError(t, err)
	t.Cleanup(func() {
		flush()
		sentrygo.CurrentHub().BindClient(nil)
	})

	return transport
}

//nolint:paralleltest // binds the global Sentry hub
func TestInitDisabledIsNoop(t *testing.T) {
	flush, err := Init(config.Config{}, "v1")
	require.NoError(t, err)
	require.NotNil(t, flush)
	flush()
	require.Nil(t, sentrygo.CurrentHub().Client())
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerReportsErrorRecordsWithRedaction(t *testing.T) {
	transport := enable(t)
	logger := logging.NewTo(io.Discard, "production", NewHandler())

	logger.Info("request ok")
	logger.Warn("slow request")
	logger.With("component", "db").Error("query failed",
		"error", errors.New("connection refused"),
		"password", "hunter2",
		slog.Group("session", "refresh_token", "raw", "user_agent", "curl"),
	)

	events := transport.Events()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, "query failed", event.Message)
	require.Equal(t, sentrygo.LevelError, event.Level)
	require.Equal(t, "v1.2.3", event.Release)
	require.NotEmpty(t, event.Exception)
	require.Equal(t, "connection refused", event.Exception[len(event.Exception)-1].Value)

	logCtx := event.Contexts["log"]
	require.Equal(t, "db", logCtx["component"])
	require.Equal(t, "[REDACTED]", logCtx["password"])
	require.Equal(t, "[REDACTED]", logCtx["session.refresh_token"])
	require.Equal(t, "curl", logCtx["session.user_agent"])
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerSkipsReportedContext(t *testing.T) {
	transport := enable(t)
	logger := logging.NewTo(io.Discard, "production", NewHandler())

	logger.ErrorContext(logging.MarkReported(context.Background()), "already reported")
	require.Empty(t, transport.Events())
}

func TestScrubRemovesSensitiveRequestData(t *testing.T) {
	t.Parallel()

	event := &sentrygo.Event{Request: &sentrygo.Request{
		URL:         "https://api.example.com/api/v1/auth/password/reset",
		QueryString: "token=secret-reset",
		Cookies:     "session=abc",
		Headers: map[string]string{
			"Authorization":  "Bearer abc",
			"X-Csrf-Token":   "csrf",
			"X-Request-Id":   "req-1",
			"Content-Type":   "application/json",
			"X-Api-Key":      "key",
			"X-Old-Password": "pw",
		},
	}}

	req := scrub(event, nil).Request
	require.Empty(t, req.QueryString)
	require.Empty(t, req.Cookies)
	require.Equal(t, map[string]string{
		"X-Request-Id": "req-1",
		"Content-Type": "application/json",
	}, req.Headers)
}
