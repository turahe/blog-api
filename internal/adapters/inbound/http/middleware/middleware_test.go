package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	"github.com/turahe/blog-api/internal/platform/logging"
)

func accessLoggedRouter(logs *bytes.Buffer) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(AccessLog(slog.New(slog.NewJSONHandler(logs, nil))))
	router.GET("/reset/:token", func(c *gin.Context) {
		responses.Internal(c, errors.New("dial tcp 10.0.0.5:5432: connection refused"), "Failed to load token")
	})

	return router
}

func TestAccessLogRecordsServerErrorCause(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	recorder := httptest.NewRecorder()
	accessLoggedRouter(&logs).ServeHTTP(recorder,
		httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/reset/secret-token", nil))

	require.Equal(t, nethttp.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "connection refused")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "ERROR", entry["level"])
	require.Equal(t, "dial tcp 10.0.0.5:5432: connection refused", entry["error"])
	require.Equal(t, "/reset/:token", entry["route"])
	require.NotContains(t, logs.String(), "secret-token")
}

func TestRequestIDPropagatesToRequestContext(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer

	logger := logging.NewTo(&logs, "production")
	router := gin.New()
	router.Use(RequestID())
	router.GET("/work", func(c *gin.Context) {
		logger.InfoContext(c.Request.Context(), "core work")
		c.Status(nethttp.StatusNoContent)
	})

	const id = "7f0c1a52-2b8e-4a39-9d3e-3c1f0e5b6a10"

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/work", nil)
	req.Header.Set("X-Request-ID", id)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, id, recorder.Header().Get("X-Request-ID"))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, id, entry["request_id"])
}

func TestAccessLogUnmatchedRouteLogsPathAtInfo(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	accessLoggedRouter(&logs).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/missing", nil))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "INFO", entry["level"])
	require.Equal(t, "/missing", entry["path"])
	require.NotContains(t, entry, "error")
}

func TestAccessLogIncludesRouteMetadata(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer

	router := gin.New()
	router.Use(AccessLog(slog.New(slog.NewJSONHandler(&logs, nil))))
	router.GET("/posts",
		routes.WithMeta(routes.Route{OperationID: "public.posts.list", Group: routes.GroupPublic, Auth: routes.AuthNone}),
		func(c *gin.Context) { c.Status(nethttp.StatusOK) })

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/posts", nil))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	assert.Equal(t, "public.posts.list", entry["operation_id"])
	assert.Equal(t, string(routes.GroupPublic), entry["route_group"])
	assert.Equal(t, string(routes.AuthNone), entry["auth_mode"])
	assert.Equal(t, "/posts", entry["route"])
}

func TestRecovery(t *testing.T) {
	t.Parallel()

	serve := func(t *testing.T, logs *bytes.Buffer, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		gin.SetMode(gin.TestMode)

		router := gin.New()
		router.Use(Recovery(slog.New(slog.NewJSONHandler(logs, nil))))
		router.GET("/x", handler)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil))

		return w
	}

	t.Run("no panic passes through", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer

		w := serve(t, &logs, func(c *gin.Context) { c.String(nethttp.StatusOK, "fine") })
		assert.Equal(t, nethttp.StatusOK, w.Code)
		assert.Equal(t, "fine", w.Body.String())
		assert.Empty(t, logs.String())
	})

	t.Run("panic becomes a 500 envelope", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer

		w := serve(t, &logs, func(*gin.Context) { panic("boom") })
		assert.Equal(t, nethttp.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), responses.ErrorCodeInternal)
		assert.Contains(t, logs.String(), "panic recovered")
	})

	t.Run("panic after writing keeps the partial response", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer

		w := serve(t, &logs, func(c *gin.Context) {
			c.String(nethttp.StatusAccepted, "partial")
			panic("late boom")
		})
		assert.Equal(t, nethttp.StatusAccepted, w.Code)
		assert.Equal(t, "partial", w.Body.String())
		assert.Contains(t, logs.String(), "late boom")
	})

	t.Run("abort handler panic is re-raised", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer

		assert.PanicsWithValue(t, nethttp.ErrAbortHandler, func() {
			serve(t, &logs, func(*gin.Context) { panic(nethttp.ErrAbortHandler) })
		})
		assert.Empty(t, logs.String())
	})
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		wantCSP string
	}{
		{name: "api", path: "/api/v1/posts", wantCSP: "default-src 'none'; frame-ancestors 'none'"},
		{name: "swagger ui root", path: "/swagger", wantCSP: swagger.ContentSecurityPolicy},
		{name: "swagger ui asset", path: "/swagger/index.html", wantCSP: swagger.ContentSecurityPolicy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gin.SetMode(gin.TestMode)

			router := gin.New()
			router.Use(SecurityHeaders())
			router.GET(tt.path, func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, tt.path, nil))

			require.Equal(t, nethttp.StatusNoContent, w.Code)
			assert.Equal(t, tt.wantCSP, w.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
			assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
		})
	}
}
