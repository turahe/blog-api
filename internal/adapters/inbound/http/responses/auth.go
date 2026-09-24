// Package responses builds the { ok, code, data, meta, error } response envelopes and resource serializers.
package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

// TokenPair serializes an auth token pair for login/refresh responses.
func TokenPair(pair authdomain.TokenPair) gin.H {
	return gin.H{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"token_type":    pair.TokenType,
		"expires_in":    pair.ExpiresIn,
	}
}

// RFC3339 formats t in UTC RFC3339, or nil when t is nil.
func RFC3339(t *time.Time) any {
	if t == nil {
		return nil
	}

	return t.UTC().Format(time.RFC3339)
}
