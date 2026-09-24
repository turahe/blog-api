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
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
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
