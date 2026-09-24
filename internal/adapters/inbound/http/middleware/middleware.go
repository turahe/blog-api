package middleware

import (
	"errors"
	"log/slog"
	nethttp "net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// RequestID attaches a correlation id to the context and response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}

		c.Set(responses.ContextRequestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// AccessLog logs method, route, status, latency, and route metadata when present.
// Matched requests log the route template rather than the raw path, which can
// carry secrets such as password reset tokens. 5xx responses log at Error level
// with the errors recorded via responses.RecordError.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()

		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			"request_id", responses.RequestID(c),
			"method", c.Request.Method,
			"status", status,
			"latency_ms", time.Since(started).Milliseconds(),
		}

		if route := c.FullPath(); route != "" {
			attrs = append(attrs, "route", route)
		} else {
			attrs = append(attrs, "path", c.Request.URL.Path)
		}

		if route, ok := routes.RouteOf(c); ok {
			attrs = append(attrs,
				"operation_id", route.OperationID,
				"route_group", string(route.Group),
				"auth_mode", string(route.Auth),
			)
		}

		if len(c.Errors) > 0 {
			errs := make([]error, 0, len(c.Errors))
			for _, e := range c.Errors {
				errs = append(errs, e.Err)
			}

			attrs = append(attrs, "error", errors.Join(errs...))
		}

		level := slog.LevelInfo
		if status >= nethttp.StatusInternalServerError {
			level = slog.LevelError
		}

		logger.Log(c.Request.Context(), level, "http request", attrs...)
	}
}

// Recovery catches panics and returns a 500 envelope.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			// http.ErrAbortHandler deliberately aborts the response; net/http handles it.
			if err, ok := recovered.(error); ok && errors.Is(err, nethttp.ErrAbortHandler) {
				panic(recovered)
			}

			logger.ErrorContext(c.Request.Context(), "panic recovered",
				"request_id", responses.RequestID(c),
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
			// The resulting 500 access log must not report this panic a second time.
			c.Request = c.Request.WithContext(logging.MarkReported(c.Request.Context()))

			if c.Writer.Written() {
				c.Abort()
				return
			}

			responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "An unexpected error occurred")
		}()

		c.Next()
	}
}

// SecurityHeaders sets baseline browser security headers (CSP relaxed on Swagger UI).
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
