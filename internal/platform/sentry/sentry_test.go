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

func (f *fakeTransport) OfType(kind string) []*sentrygo.Event {
	var out []*sentrygo.Event

	for _, e := range f.Events() {
		if e.Type == kind {
			out = append(out, e)
		}
	}

	return out
}

// enable binds a global Sentry client backed by a fake transport for one test.
func enable(t *testing.T) *fakeTransport {
	t.Helper()

	transport, _ := enableWith(t, config.Config{SentryLogsLevel: "off"})

	return transport
}

// enableWith is enable with extra settings; it also returns the flush func.
func enableWith(t *testing.T, cfg config.Config) (*fakeTransport, func()) {
	t.Helper()

	cfg.SentryDSN = "https://public@example.com/1"
	cfg.SentryEnvironment = "test"

	transport := &fakeTransport{}
	flush, err := initWithTransport(cfg, "v1.2.3", transport)
	require.NoError(t, err)
	t.Cleanup(func() {
		flush()

		c := sentrygo.CurrentHub().Client()
		sentrygo.CurrentHub().BindClient(nil)

		if c != nil {
			c.Close()
		}
	})

	return transport, flush
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
func TestInitReportsInvalidDSN(t *testing.T) {
	flush, err := Init(config.Config{SentryDSN: "not a dsn"}, "v1")
	require.ErrorContains(t, err, "init sentry")
	require.Nil(t, flush)
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

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerWithLogsSendsRecordsAtLevel(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "info"})
	logger := logging.NewTo(io.Discard, "local", NewHandler().WithLogs(slog.LevelInfo))

	logger.Debug("cache miss")
	logger.With("component", "http").Info("request ok",
		"status", 200, "cached", true, "access_token", "raw")
	logger.ErrorContext(logging.MarkReported(context.Background()), "panic recovered")
	flush()

	var logs []sentrygo.Log
	for _, e := range transport.OfType("log") {
		logs = append(logs, e.Logs...)
	}

	require.Len(t, logs, 2, "debug is below the logs level")

	info := logs[0]
	require.Equal(t, "request ok", info.Body)
	require.Equal(t, sentrygo.LogLevelInfo, info.Level)
	require.Equal(t, "http", info.Attributes["component"].AsString())
	require.Equal(t, int64(200), info.Attributes["status"].AsInt64())
	require.True(t, info.Attributes["cached"].AsBool())
	require.Equal(t, "[REDACTED]", info.Attributes["access_token"].AsString())
	require.Equal(t, "v1.2.3", info.Attributes["sentry.release"].AsString())

	require.Equal(t, sentrygo.LogLevelError, logs[1].Level)
	require.Empty(t, transport.OfType(""), "a reported record is still logged but not captured again")
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerWithoutLogsSendsNoLogs(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "off"})
	logger := logging.NewTo(io.Discard, "production", NewHandler())

	logger.Info("request ok")
	flush()

	require.Empty(t, transport.OfType("log"))
}

//nolint:paralleltest // reads the global Sentry hub
func TestHandlerWithoutClientCapturesNothing(t *testing.T) {
	require.Nil(t, sentrygo.CurrentHub().Client())

	record := slog.NewRecord(time.Now(), slog.LevelError, "boom", 0)
	require.NoError(t, NewHandler().Handle(context.Background(), record))
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerWithGroupPrefixesKeys(t *testing.T) {
	transport := enable(t)
	handler := NewHandler()

	require.Same(t, handler, handler.WithGroup(""), "an empty group is a no-op")

	slog.New(handler).WithGroup("job").With("name", "prune").WithGroup("db").
		Error("query failed", "err", errors.New("deadlock"), "table", "posts")

	events := transport.Events()
	require.Len(t, events, 1)

	event := events[0]
	require.NotEmpty(t, event.Exception, "a grouped err attribute is still the exception")
	require.Equal(t, "deadlock", event.Exception[len(event.Exception)-1].Value)
	require.Equal(t, "prune", event.Contexts["log"]["job.name"])
	require.Equal(t, "posts", event.Contexts["log"]["job.db.table"])
	require.Contains(t, event.Contexts["log"], "job.db.err")
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerSkipsAttributesWithoutKey(t *testing.T) {
	transport := enable(t)

	record := slog.NewRecord(time.Now(), slog.LevelError, "boom", 0)
	record.AddAttrs(slog.String("", "orphan"), slog.String("kept", "yes"))
	require.NoError(t, NewHandler().Handle(context.Background(), record))

	events := transport.Events()
	require.Len(t, events, 1)
	require.Equal(t, sentrygo.Context{"kept": "yes"}, events[0].Contexts["log"])
}

//nolint:paralleltest // binds the global Sentry hub
func TestHandlerWithLogsMapsLevelsAndFloats(t *testing.T) {
	transport, flush := enableWith(t, config.Config{SentryLogsLevel: "debug"})
	logger := slog.New(NewHandler().WithLogs(slog.LevelDebug))

	logger.Debug("cache miss", "ratio", 0.25)
	logger.Warn("slow query")
	flush()

	var logs []sentrygo.Log
	for _, e := range transport.OfType("log") {
		logs = append(logs, e.Logs...)
	}

	require.Len(t, logs, 2)
	require.Equal(t, sentrygo.LogLevelDebug, logs[0].Level)
	require.InDelta(t, 0.25, logs[0].Attributes["ratio"].AsFloat64(), 1e-9)
	require.Equal(t, sentrygo.LogLevelWarn, logs[1].Level)
	require.Empty(t, transport.OfType(""), "records below Error are not captured as events")
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
