package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

// Category serializes a category resource.
func Category(cat categorydomain.Category) gin.H {
	var parent any
	if cat.ParentID != nil {
		parent = cat.ParentID.String()
	}
	var image any
	if cat.ImageID != nil {
		image = cat.ImageID.String()
	}
	return gin.H{
		"id":          cat.ID.String(),
		"name":        cat.Name,
		"slug":        cat.Slug,
		"description": cat.Description,
		"parent_id":   parent,
		"image_id":    image,
		"lft":         cat.Lft,
		"rgt":         cat.Rgt,
		"depth":       cat.Depth,
		"sort_order":  cat.SortOrder,
		"created_at":  cat.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
