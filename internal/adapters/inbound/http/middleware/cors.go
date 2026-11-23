package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

const corsAllowHeaders = "Authorization, Content-Type, Accept, Origin, X-Requested-With, X-CSRF-Token, X-Request-ID, X-2FA-Code, Last-Event-ID, Cache-Control"
const corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// CORS reflects allowlisted Origins and short-circuits OPTIONS preflight.
// Requests without an Origin header pass through (non-browser clients).
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}

		if _, ok := allowed[origin]; !ok {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}

			responses.Failure(c, http.StatusForbidden, responses.ErrorCodeCORSOrigin, "Origin is not allowed")
			c.Abort()
			return
		}

		header := c.Writer.Header()
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Allow-Credentials", "true")
		header.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		header.Set("Access-Control-Allow-Methods", corsAllowMethods)
		header.Set("Access-Control-Expose-Headers", "X-Request-ID")
		header.Add("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
