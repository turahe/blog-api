package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// User serializes a user resource.
func User(user userdomain.User) gin.H {
	return gin.H{
		"id":                user.ID.String(),
		"email":             user.Email,
		"username":          user.Username,
		"full_name":         user.FullName,
		"status":            string(user.Status),
		"email_verified_at": RFC3339(user.EmailVerifiedAt),
		"login_count":       user.LoginCount,
		"created_at":        user.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":        user.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
