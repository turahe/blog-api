package middleware

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/logging"
	sentryplatform "github.com/turahe/blog-api/internal/platform/sentry"
)

type captureTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (c *captureTransport) Flush(time.Duration) bool              { return true }
func (c *captureTransport) FlushWithContext(context.Context) bool { return true }
func (c *captureTransport) Configure(sentry.ClientOptions)        {}
func (c *captureTransport) Close()                                {}

func (c *captureTransport) SendEvent(event *sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)
}

func (c *captureTransport) split() (errs, txs []*sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, e := range c.events {
		if e.Type == "transaction" {
			txs = append(txs, e)
		} else {
			errs = append(errs, e)
		}
	}

	return errs, txs
}

//nolint:paralleltest // binds the global Sentry hub
func TestTracingReportsPanicOnceWithRouteTransaction(t *testing.T) {
	transport := &captureTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:              "https://public@example.com/1",
		EnableTracing:    true,
		TracesSampleRate: 1,
		Transport:        transport,
	}))
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	gin.SetMode(gin.TestMode)

	logger := logging.NewTo(io.Discard, "production", sentryplatform.NewHandler())
	router := gin.New()
	router.Use(RequestID(), Tracing(), AccessLog(logger), Recovery(logger))
	router.GET("/posts/:slug", func(*gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/posts/hello?token=abc", nil))
	require.Equal(t, nethttp.StatusInternalServerError, rec.Code)

	errs, txs := transport.split()
	require.Len(t, errs, 1)
	require.Equal(t, "panic recovered", errs[0].Message)
	require.NotEmpty(t, errs[0].Tags["request_id"])

	require.Len(t, txs, 1)
	require.Equal(t, "GET /posts/:slug", txs[0].Transaction)
	require.Equal(t, txs[0].Contexts["trace"]["trace_id"], errs[0].Contexts["trace"]["trace_id"])
}

func TestTracingNoopWithoutSentry(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Tracing())
	router.GET("/ok", func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/ok", nil))
	require.Equal(t, nethttp.StatusNoContent, rec.Code)
}
