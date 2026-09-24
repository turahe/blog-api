package middleware

import (
	"context"
	"log/slog"
	"math"
	nethttp "net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// Limiter counts hits per key within a window.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// RateLimit allows limit requests per window for each caller of the named bucket. Callers are
// the authenticated user when present, otherwise the client IP. A limiter error lets the
// request through: throttling must not take the endpoint down with Redis.
func RateLimit(limiter Limiter, logger *slog.Logger, bucket string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || limit < 1 {
			c.Next()
			return
		}

		caller := "ip:" + c.ClientIP()
		if userID, ok := CurrentUserID(c); ok {
			caller = "user:" + userID.String()
		}

		allowed, retryAfter, err := limiter.Allow(c.Request.Context(), bucket+":"+caller, limit, window)
		if err != nil {
			if logger != nil {
				logger.WarnContext(c.Request.Context(), "rate limiter unavailable", "bucket", bucket, "error", err)
			}

			c.Next()

			return
		}

		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			c.Header("Retry-After", strconv.Itoa(max(seconds, 1)))
			responses.Failure(c, nethttp.StatusTooManyRequests, "rate_limited", "Too many requests, retry later")

			return
		}

		c.Next()
	}
}
