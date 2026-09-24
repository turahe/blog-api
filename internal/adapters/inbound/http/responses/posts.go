package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// Post serializes a post resource.
func Post(post postdomain.Post) gin.H {
	var categoryID any
	if post.CategoryUUID != nil {
		categoryID = post.CategoryUUID.String()
	}
	var coverImageMediaID any
	if post.CoverImageMediaUUID != nil {
		coverImageMediaID = post.CoverImageMediaUUID.String()
	}
	return gin.H{
		"id":                   post.UUID.String(),
		"author_id":            post.AuthorUUID.String(),
		"category_id":          categoryID,
		"title":                post.Title,
		"slug":                 post.Slug,
		"excerpt":              post.Excerpt,
		"content":              post.Content,
		"cover_image_media_id": coverImageMediaID,
		"status":               string(post.Status),
		"published_at":         RFC3339(post.PublishedAt),
		"created_at":           post.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":           post.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// PostWithTags serializes a post including its tags.
func PostWithTags(post postdomain.Post, tags []tagdomain.Tag) gin.H {
	payload := Post(post)
	if tags == nil {
		payload["tags"] = []gin.H{}
		return payload
	}
	encoded := make([]gin.H, 0, len(tags))
	for _, tag := range tags {
		encoded = append(encoded, Tag(tag))
	}
	payload["tags"] = encoded
	return payload
}
