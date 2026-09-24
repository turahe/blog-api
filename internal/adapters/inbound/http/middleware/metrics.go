package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

// unmatchedRoute labels requests that matched no route, keeping label cardinality bounded.
const unmatchedRoute = "unmatched"

// MetricsRecorder receives one observation per HTTP request.
type MetricsRecorder interface {
	StartRequest() func()
	ObserveHTTP(method, route string, status int, elapsed time.Duration)
}

// Metrics records request count, latency, and in-flight requests by route template.
func Metrics(recorder MetricsRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		done := recorder.StartRequest()
		started := time.Now()

		defer func() {
			done()

			route := c.FullPath()
			if route == "" {
				route = unmatchedRoute
			}

			recorder.ObserveHTTP(c.Request.Method, route, c.Writer.Status(), time.Since(started))
		}()

		c.Next()
	}
}
