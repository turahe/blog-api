package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

const (
	contextUserIDKey    = "auth_user_id"
	contextClaimsKey    = "auth_claims"
	authorizationHeader = "Authorization"
)

func bearerAuthMiddleware(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			failure(c, 401, "unauthorized", "Authentication required")
			return
		}
		raw := strings.TrimSpace(header[len("Bearer "):])
		claims, err := auth.ParseAccessToken(raw)
		if err != nil {
			failure(c, 401, "unauthorized", "Invalid or expired access token")
			return
		}
		c.Set(contextUserIDKey, claims.Subject)
		c.Set(contextClaimsKey, claims)
		c.Next()
	}
}

func optionalBearerAuthMiddleware(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authorizationHeader)
		if header != "" && strings.HasPrefix(strings.ToLower(header), "bearer ") {
			raw := strings.TrimSpace(header[len("Bearer "):])
			if claims, err := auth.ParseAccessToken(raw); err == nil {
				c.Set(contextUserIDKey, claims.Subject)
				c.Set(contextClaimsKey, claims)
			}
		}
		c.Next()
	}
}

func currentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, ok := c.Get(contextUserIDKey)
	if !ok {
		return uuid.Nil, false
	}
	id, ok := value.(uuid.UUID)
	return id, ok
}
