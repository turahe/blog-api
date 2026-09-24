package middleware

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
)

func TestCacheBypassHonoursNoCacheHeaderOnly(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	cases := map[string]struct {
		header string
		mw     gin.HandlerFunc
		want   bool
	}{
		"no header":            {"", CacheBypass(), false},
		"max-age":              {"max-age=0", CacheBypass(), false},
		"no-cache":             {"no-cache", CacheBypass(), true},
		"mixed case directive": {"max-age=0, No-Cache", CacheBypass(), true},
		"admin always bypass":  {"", NoReadCache(), true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var bypassed bool

			router := gin.New()
			router.GET("/", tc.mw, func(c *gin.Context) {
				bypassed = readcache.Bypassed(c.Request.Context())
				c.Status(nethttp.StatusNoContent)
			})

			req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Cache-Control", tc.header)
			}

			router.ServeHTTP(httptest.NewRecorder(), req)
			require.Equal(t, tc.want, bypassed)
		})
	}
}
