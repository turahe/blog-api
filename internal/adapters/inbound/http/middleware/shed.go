package middleware

import (
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// MaxInFlight answers 503 with Retry-After once limit requests are being served, so an
// overloaded replica sheds load instead of queueing until clients time out. Health probes and
// long-lived event streams are not counted. A limit below 1 disables shedding.
func MaxInFlight(limit int) gin.HandlerFunc {
	if limit < 1 {
		return func(c *gin.Context) { c.Next() }
	}

	slots := make(chan struct{}, limit)

	return func(c *gin.Context) {
		if exemptFromShedding(c.Request.URL.Path) {
			c.Next()
			return
		}

		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()

			c.Next()
		default:
			c.Header("Retry-After", "1")
			responses.Failure(c, nethttp.StatusServiceUnavailable, "server.overloaded", "Server is busy, retry shortly")
		}
	}
}

func exemptFromShedding(path string) bool {
	return strings.HasPrefix(path, "/health/") ||
		strings.HasPrefix(path, "/api/v1/health") ||
		strings.HasSuffix(path, "/stream")
}
