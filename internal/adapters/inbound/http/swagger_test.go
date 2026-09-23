package http

import (
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
)

func TestSwaggerEnabledServesUIAndSpec(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:         healthservice.New("test"),
		Version:        "test",
		SwaggerEnabled: true,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, "/openapi.yaml", nil))
	require.Equal(t, nethttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "openapi:")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "unpkg.com")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, "/swagger/index.html", nil))
	require.Equal(t, nethttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "SwaggerUIBundle")
	require.Equal(t, swagger.ContentSecurityPolicy, rec.Header().Get("Content-Security-Policy"))

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, "/swagger", nil))
	require.Equal(t, nethttp.StatusFound, rec.Code)
	require.Equal(t, "/swagger/index.html", rec.Header().Get("Location"))
}

func TestSwaggerDisabledReturnsNotFound(t *testing.T) {
	router, err := NewRouter(Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:         healthservice.New("test"),
		Version:        "test",
		SwaggerEnabled: false,
	})
	require.NoError(t, err)

	for _, path := range []string{"/swagger", "/swagger/index.html", "/openapi.yaml"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, path, nil))
		require.Equal(t, nethttp.StatusNotFound, rec.Code, path)
	}
}
