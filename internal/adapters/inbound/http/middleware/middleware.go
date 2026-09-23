package middleware

import (
	"log/slog"
	nethttp "net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/swagger"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
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

// AccessLog logs method, path, status, latency, and route metadata when present.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		attrs := []any{
			"request_id", responses.RequestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(started).Milliseconds(),
		}
		if route, ok := routes.RouteOf(c); ok {
			attrs = append(attrs,
				"operation_id", route.OperationID,
				"route_group", string(route.Group),
				"auth_mode", string(route.Auth),
			)
		}
		logger.InfoContext(c.Request.Context(), "http request", attrs...)
	}
}

// Recovery catches panics and returns a 500 envelope.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(c.Request.Context(), "panic recovered",
					"request_id", responses.RequestID(c),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "An unexpected error occurred")
			}
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
