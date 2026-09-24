package middleware

import (
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// Tracing gives each request its own Sentry hub (so error events carry request
// data) and, for matched routes, a transaction named after the route template
// rather than the raw path. It is a no-op when Sentry is not initialised.
func Tracing() gin.HandlerFunc {
	return func(c *gin.Context) {
		if sentry.CurrentHub().Client() == nil {
			c.Next()
			return
		}

		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetRequest(c.Request)
		hub.Scope().SetTag("request_id", responses.RequestID(c))
		ctx := sentry.SetHubOnContext(c.Request.Context(), hub)

		route := c.FullPath()
		if route == "" {
			c.Request = c.Request.WithContext(ctx)
			c.Next()

			return
		}

		tx := sentry.StartTransaction(ctx, c.Request.Method+" "+route,
			sentry.WithOpName("http.server"),
			sentry.ContinueFromRequest(c.Request),
			sentry.WithTransactionSource(sentry.SourceRoute),
			sentry.WithSpanOrigin(sentry.SpanOriginGin),
		)
		tx.SetData("http.request.method", c.Request.Method)

		defer func() {
			status := c.Writer.Status()
			tx.Status = sentry.HTTPtoSpanStatus(status)
			tx.SetData("http.response.status_code", status)
			tx.Finish()
		}()

		c.Request = c.Request.WithContext(tx.Context())
		c.Next()
	}
}
