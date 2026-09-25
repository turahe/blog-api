package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// ImpersonationSession renders a session; active is false once it ended or expired.
func ImpersonationSession(s impdomain.Session, active bool) gin.H {
	var endReason *string

	if s.EndReason != nil {
		reason := string(*s.EndReason)
		endReason = &reason
	}

	return gin.H{
		"impersonationSessionId": s.UUID,
		"impersonatorUserId":     s.ActorUUID,
		"targetUserId":           s.TargetUUID,
		"state":                  string(s.State),
		"active":                 active,
		"reason":                 s.Reason,
		"startedAt":              s.StartedAt.UTC().Format(time.RFC3339),
		"expiresAt":              s.ExpiresAt.UTC().Format(time.RFC3339),
		"endedAt":                RFC3339(s.EndedAt),
		"endReason":              endReason,
	}
}

// ImpersonationStarted renders a new session with its target and the Bearer token that acts
// as the target until expiresAt. There is no refresh token.
func ImpersonationStarted(s impdomain.Session, target userdomain.User, token string) gin.H {
	return gin.H{
		"session": ImpersonationSession(s, true),
		"targetUser": gin.H{
			"id": target.UUID, "username": target.Username, "email": target.Email, "fullName": target.FullName,
		},
		"accessToken": token,
		"tokenType":   "Bearer",
		"expiresIn":   int64(s.ExpiresAt.Sub(s.StartedAt).Seconds()),
	}
}
