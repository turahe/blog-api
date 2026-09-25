package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// User serializes a user resource.
func User(user userdomain.User) gin.H {
	return gin.H{
		"id":              user.UUID.String(),
		"email":           user.Email,
		"username":        user.Username,
		"fullName":        user.FullName,
		"status":          string(user.Status),
		"emailVerifiedAt": RFC3339(user.EmailVerifiedAt),
		"loginCount":      user.LoginCount,
		"createdAt":       user.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt":       user.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
