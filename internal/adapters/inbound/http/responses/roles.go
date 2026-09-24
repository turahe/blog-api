package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
)

// Role serializes a role with its permission keys.
func Role(role rbacdomain.Role) gin.H {
	return gin.H{
		"id":          role.UUID.String(),
		"name":        role.Name,
		"description": role.Description,
		"permissions": role.Permissions,
		"protected":   role.Name == rbacdomain.ProtectedRole,
		"created_at":  role.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  role.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// Permission serializes a registered permission.
func Permission(p rbacdomain.Permission) gin.H {
	return gin.H{
		"id":          p.UUID.String(),
		"key":         p.Key,
		"description": p.Description,
	}
}
