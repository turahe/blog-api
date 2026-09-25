package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// Tag serializes a tag resource.
func Tag(tag tagdomain.Tag) gin.H {
	return gin.H{
		"id":        tag.UUID.String(),
		"name":      tag.Name,
		"slug":      tag.Slug,
		"createdAt": tag.CreatedAt.UTC().Format(time.RFC3339),
	}
}
