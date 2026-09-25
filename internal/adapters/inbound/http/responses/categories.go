package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

// Category serializes a category resource.
func Category(cat categorydomain.Category) gin.H {
	var parent any
	if cat.ParentUUID != nil {
		parent = cat.ParentUUID.String()
	}

	var image any
	if cat.ImageUUID != nil {
		image = cat.ImageUUID.String()
	}

	return gin.H{
		"id":          cat.UUID.String(),
		"name":        cat.Name,
		"slug":        cat.Slug,
		"description": cat.Description,
		"parentId":    parent,
		"imageId":     image,
		"lft":         cat.Lft,
		"rgt":         cat.Rgt,
		"depth":       cat.Depth,
		"sortOrder":   cat.SortOrder,
		"createdAt":   cat.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt":   cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
