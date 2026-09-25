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
		"impersonation_session_id": s.UUID,
		"impersonator_user_id":     s.ActorUUID,
		"target_user_id":           s.TargetUUID,
		"state":                    string(s.State),
		"active":                   active,
		"reason":                   s.Reason,
		"started_at":               s.StartedAt.UTC().Format(time.RFC3339),
		"expires_at":               s.ExpiresAt.UTC().Format(time.RFC3339),
		"ended_at":                 RFC3339(s.EndedAt),
		"end_reason":               endReason,
	}
}

// ImpersonationStarted renders a new session with its target and the Bearer token that acts
// as the target until expires_at. There is no refresh token.
func ImpersonationStarted(s impdomain.Session, target userdomain.User, token string) gin.H {
	return gin.H{
		"session": ImpersonationSession(s, true),
		"target_user": gin.H{
			"id": target.UUID, "username": target.Username, "email": target.Email, "full_name": target.FullName,
		},
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int64(s.ExpiresAt.Sub(s.StartedAt).Seconds()),
	}
}
