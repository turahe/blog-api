package http

import (
	"encoding/json"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
)

func TestLoginValidationReturnsLaravelStyleFieldErrors(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Auth:    fakeAuthService{},
		Version: "test",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, nethttp.StatusBadRequest, rec.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
	require.Equal(t, "The given data was invalid.", envelope.Error.Message)

	details, ok := envelope.Error.Details.(map[string]any)
	require.True(t, ok)
	require.Contains(t, details, "email")
	require.Contains(t, details, "password")
}

func TestLoginValidationRejectsInvalidEmail(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:  healthservice.New("test"),
		Auth:    fakeAuthService{},
		Version: "test",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	body := `{"email":"not-an-email","password":"secret"}`
	req := httptest.NewRequest(nethttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, nethttp.StatusBadRequest, rec.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	details, ok := envelope.Error.Details.(map[string]any)
	require.True(t, ok)
	require.Contains(t, details, "email")
}
