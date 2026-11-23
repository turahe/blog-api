package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
)

// SecurityHeaders sets baseline browser security headers (CSP relaxed on Swagger UI).
// Kept local: github.com/turahe/pkg/middlewares has no equivalent.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		csp := "default-src 'none'; frame-ancestors 'none'"
		if swagger.IsDocsPath(c.Request.URL.Path) {
			csp = swagger.ContentSecurityPolicy
		}

		c.Header("Content-Security-Policy", csp)
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}
}
