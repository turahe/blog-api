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
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"tokenType":    pair.TokenType,
		"expiresIn":    pair.ExpiresIn,
	}
}

// TwoFactorChallenge serializes the login response of a two-factor account;
// the client completes it at POST /api/v1/auth/2fa/challenge.
func TwoFactorChallenge(challenge authdomain.TwoFactorChallenge, now time.Time) gin.H {
	return gin.H{
		"twoFactorRequired": true,
		"challengeToken":    challenge.Token,
		"expiresAt":         challenge.ExpiresAt.UTC().Format(time.RFC3339),
		"expiresIn":         int64(max(challenge.ExpiresAt.Sub(now).Seconds(), 0)),
	}
}

// TwoFactorStatus serializes a user's enrollment.
func TwoFactorStatus(status authdomain.TwoFactorStatus) gin.H {
	return gin.H{
		"enabled":              status.Enabled,
		"pending":              status.Pending,
		"confirmedAt":          RFC3339(status.ConfirmedAt),
		"backupCodesRemaining": status.BackupCodesRemaining,
	}
}

// RFC3339 formats t in UTC RFC3339, or nil when t is nil.
func RFC3339(t *time.Time) any {
	if t == nil {
		return nil
	}

	return t.UTC().Format(time.RFC3339)
}
