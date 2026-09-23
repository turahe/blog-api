package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

const (
	ContextUserIDKey    = "auth_user_id"
	ContextClaimsKey    = "auth_claims"
	authorizationHeader = "Authorization"
)

// BearerAuth requires a valid Bearer access token.
func BearerAuth(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			responses.Failure(c, 401, "unauthorized", "Authentication required")
			return
		}
		raw := strings.TrimSpace(header[len("Bearer "):])
		claims, err := auth.ParseAccessToken(raw)
		if err != nil {
			responses.Failure(c, 401, "unauthorized", "Invalid or expired access token")
			return
		}
		c.Set(ContextUserIDKey, claims.Subject)
		c.Set(ContextClaimsKey, claims)
		c.Next()
	}
}

// OptionalBearerAuth attaches identity when a valid token is present; never 401s.
func OptionalBearerAuth(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header != "" && strings.HasPrefix(strings.ToLower(header), "bearer ") {
			raw := strings.TrimSpace(header[len("Bearer "):])
			if claims, err := auth.ParseAccessToken(raw); err == nil {
				c.Set(ContextUserIDKey, claims.Subject)
				c.Set(ContextClaimsKey, claims)
			}
		}
		c.Next()
	}
}

// CurrentUserID returns the authenticated subject from context.
func CurrentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(ContextUserIDKey)
	if !ok {
		return uuid.Nil, false
	}
	id, ok := value.(uuid.UUID)
	return id, ok
}
