package http

import (
	"log/slog"
	nethttp "net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	v1 "github.com/turahe/blog-api/internal/adapters/inbound/http/v1"
)

const requestIDKey = "request_id"

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func accessLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		attrs := []any{
			"request_id", requestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(started).Milliseconds(),
		}
		if route, ok := v1.RouteOf(c); ok {
			attrs = append(attrs,
				"operation_id", route.OperationID,
				"route_group", string(route.Group),
				"auth_mode", string(route.Auth),
			)
		}
		logger.InfoContext(c.Request.Context(), "http request", attrs...)
	}
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(c.Request.Context(), "panic recovered",
					"request_id", requestID(c),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				failure(c, nethttp.StatusInternalServerError, "internal_error", "An unexpected error occurred")
			}
		}()
		c.Next()
	}
}

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	id, _ := value.(string)
	return id
}
