package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
)

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
