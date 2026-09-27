package sentry

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	sentrygo "github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// accessAttrs mirrors the attributes of one middleware.AccessLog record.
var accessAttrs = []any{
	"request_id", "0192f7c4-7d7e-7a3b-9f3e-2c1d5e6f7a8b",
	"method", "GET",
	"status", 200,
	"latency_ms", int64(12),
	"route", "/api/v1/posts/:slug",
	"operation_id", "public.posts.get",
	"route_group", "public",
	"auth_mode", "anonymous",
}

// benchSentry initialises Sentry like production (default HTTP transport and
// telemetry buffer) against a local server that discards every envelope.
func benchSentry(b *testing.B, cfg config.Config) {
	b.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	b.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(b, err)

	cfg.SentryDSN = "http://public@" + u.Host + "/1"
	cfg.SentryEnvironment = "bench"

	flush, err := Init(cfg, "bench")
	require.NoError(b, err)
	b.Cleanup(func() {
		flush()
		sentrygo.CurrentHub().BindClient(nil)
	})
}

func BenchmarkHandle(b *testing.B) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelError} {
		for _, mode := range []string{"off", "events", "logs"} {
			b.Run(fmt.Sprintf("level=%s/sentry=%s", level, mode), func(b *testing.B) {
				var extra []slog.Handler

				switch mode {
				case "events":
					benchSentry(b, config.Config{SentryLogsLevel: "off"})

					extra = append(extra, NewHandler())
				case "logs":
					benchSentry(b, config.Config{SentryLogsLevel: "info"})

					extra = append(extra, NewHandler().WithLogs(slog.LevelInfo))
				}

				logger := logging.NewTo(io.Discard, "production", extra...)
				ctx := sentrygo.SetHubOnContext(b.Context(), sentrygo.CurrentHub().Clone())

				attrs := accessAttrs
				if level >= slog.LevelError {
					attrs = append(append([]any{}, accessAttrs...), "error", errors.New("upstream timeout"))
				}

				b.ReportAllocs()

				for b.Loop() {
					logger.Log(ctx, level, "request", attrs...)
				}
			})
		}
	}
}
