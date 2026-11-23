package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	pkgconfig "github.com/turahe/pkg/config"
	pklogger "github.com/turahe/pkg/logger"
	pkgmw "github.com/turahe/pkg/middlewares"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// PkgStack is the turahe/pkg production middleware order (recovery → trace →
// instrumentation → logger → metrics → timeout → CORS → mTLS → rate limit), plus a
// bridge that copies the correlation id onto the Gin context for this API's envelope.
// Domain middleware (SecurityHeaders, MaxInFlight, Audit, CacheBypass, Sentry Tracing)
// is layered by the caller.
func PkgStack(requestTimeout time.Duration) gin.HandlersChain {
	chain := gin.HandlersChain{
		pkgmw.RecoveryHandler,
		pkgmw.TraceMiddleware(),
		BridgeRequestID(),
		pkgmw.HTTPInstrumentation(),
		pkgmw.LoggerMiddleware(),
		pkgmw.Metrics(),
		pkgmw.RequestTimeout(requestTimeout),
		pkgmw.CORS(),
	}

	if cfg := pkgconfig.GetConfig(); cfg != nil && cfg.MTLS.Enabled {
		chain = append(chain, pkgmw.MTLSMiddleware())
	}

	if cfg := pkgconfig.GetConfig(); cfg != nil && cfg.RateLimiter.Enabled {
		chain = append(chain, pkgmw.RateLimiter())
	}

	return chain
}

// BridgeRequestID copies the pkg trace/correlation id onto responses.ContextRequestIDKey
// so handlers and the response envelope keep working.
func BridgeRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Writer.Header().Get(pkgmw.HeaderRequestID)
		if id == "" {
			id = pklogger.GetCorrelationID(c.Request.Context())
		}
		if id == "" {
			id = pklogger.GetTraceID(c.Request.Context())
		}
		if id != "" {
			c.Set(responses.ContextRequestIDKey, id)
		}
		c.Next()
	}
}
