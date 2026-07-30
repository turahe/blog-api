package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
)

func TestHealthLive(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodGet, "/health/live", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusOK, recorder.Code)
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.NotEmpty(t, recorder.Header().Get("X-Request-ID"))

	var envelope Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
}

func TestContractOperationIsRegistered(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/oauth/google/callback", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)

	var envelope Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "operation.not_implemented", envelope.Error.Code)
	require.Equal(t, map[string]any{
		"operation_id": "auth.oauth.callback",
		"route_group":  "auth",
		"auth_mode":    "none",
	}, envelope.Error.Details)
}

func TestLoginValidationWithoutAuthService(t *testing.T) {
	// Without Auth wired, login stays a contract stub.
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, nethttp.StatusNotImplemented, recorder.Code)
}

func TestAccessLogCarriesRouteGroup(t *testing.T) {
	var logs bytes.Buffer
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
		Health:  healthservice.New("test"),
		Version: "test",
	})
	require.NoError(t, err)

	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(nethttp.MethodGet, "/api/v1/admin/users", nil),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	require.Equal(t, "admin.users.list", entry["operation_id"])
	require.Equal(t, "admin", entry["route_group"])
	require.Equal(t, "required", entry["auth_mode"])
}
