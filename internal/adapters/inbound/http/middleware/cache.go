package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/core/readcache"
)

// CacheBypass lets a request skip the public read cache with `Cache-Control: no-cache`.
// Leave it off in production: every bypassed request goes to the database.
func CacheBypass() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.Contains(strings.ToLower(c.GetHeader("Cache-Control")), "no-cache") {
			c.Request = c.Request.WithContext(readcache.WithBypass(c.Request.Context()))
		}

		c.Next()
	}
}

// NoReadCache makes every request skip the public read cache, e.g. on admin reads
// that share a cached service method with the public API.
func NoReadCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(readcache.WithBypass(c.Request.Context()))
		c.Next()
	}
}
