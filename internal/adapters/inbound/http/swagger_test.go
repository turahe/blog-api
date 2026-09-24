package http

import (
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	healthservice "github.com/turahe/blog-api/internal/core/health/service"
)

func TestSwaggerEnabledServesUIAndSpec(t *testing.T) {
	t.Parallel()

	router, err := NewRouter(Dependencies{
		Logger:         slog.Default(),
		Health:         healthservice.New("test"),
		SwaggerEnabled: true,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/swagger/index.html", nil))
	require.Equal(t, nethttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "swagger")
	require.Equal(t, swagger.ContentSecurityPolicy, rec.Header().Get("Content-Security-Policy"))

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/swagger/doc.json", nil))
	require.Equal(t, nethttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"swagger"`)
	require.Contains(t, rec.Body.String(), "Blog API")
}

func TestSwaggerDisabledReturnsNotFound(t *testing.T) {
	t.Parallel()

	router, err := NewRouter(Dependencies{
		Logger:         slog.Default(),
		Health:         healthservice.New("test"),
		SwaggerEnabled: false,
	})
	require.NoError(t, err)

	for _, path := range []string{"/swagger/index.html", "/swagger/doc.json"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, path, nil))
		require.Equal(t, nethttp.StatusNotFound, rec.Code, path)
	}
}
