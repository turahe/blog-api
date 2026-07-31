package http

import (
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type createPostRequest struct {
	Title      string  `json:"title"`
	Slug       string  `json:"slug"`
	Excerpt    string  `json:"excerpt"`
	Content    string  `json:"content"`
	CategoryID *string `json:"category_id"`
}

func listCategoriesHandler(cats *categoryservice.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := cats.List(c.Request.Context())
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list categories")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, item := range items {
			out = append(out, categoryJSON(item))
		}
		success(c, nethttp.StatusOK, out)
	}
}

func getCategoryHandler(cats *categoryservice.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		item, err := cats.GetBySlug(c.Request.Context(), c.Param("param1"))
		if errors.Is(err, categorydomain.ErrNotFound) {
			failure(c, nethttp.StatusNotFound, "not_found", "Category not found")
			return
		}
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load category")
			return
		}
		success(c, nethttp.StatusOK, categoryJSON(item))
	}
}

func adminCreatePostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		var req createPostRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		var categoryID *uuid.UUID
		if req.CategoryID != nil && strings.TrimSpace(*req.CategoryID) != "" {
			id, err := uuid.Parse(strings.TrimSpace(*req.CategoryID))
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
				return
			}
			categoryID = &id
		}
		post, err := posts.CreateDraft(c.Request.Context(), userID, req.Title, req.Slug, req.Excerpt, req.Content, categoryID)
		if errors.Is(err, postservice.ErrValidation) {
			failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to create post")
			return
		}
		success(c, nethttp.StatusCreated, postJSON(post))
	}
}

func adminPublishPostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}
		post, err := posts.Publish(c.Request.Context(), id)
		if errors.Is(err, postdomain.ErrNotFound) {
			failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to publish post")
			return
		}
		success(c, nethttp.StatusOK, postJSON(post))
	}
}

type postMediaReplaceRequest struct {
	EnforceCoverConsistency *bool `json:"enforce_cover_consistency"`
	Items                   []struct {
		MediaAssetID string `json:"media_asset_id"`
		Kind         string `json:"kind"`
		SortOrder    int    `json:"sort_order"`
	} `json:"items"`
}

func adminReplacePostMediaHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}
		var req postMediaReplaceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		enforce := true
		if req.EnforceCoverConsistency != nil {
			enforce = *req.EnforceCoverConsistency
		}
		items := make([]mediadomain.PostMediaItem, 0, len(req.Items))
		for _, item := range req.Items {
			mediaID, err := uuid.Parse(strings.TrimSpace(item.MediaAssetID))
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media_asset_id")
				return
			}
			items = append(items, mediadomain.PostMediaItem{
				MediaAssetID: mediaID,
				Kind:         item.Kind,
				SortOrder:    item.SortOrder,
			})
		}
		out, err := posts.ReplaceMedia(c.Request.Context(), postID, items, enforce)
		if errors.Is(err, postdomain.ErrNotFound) {
			failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if errors.Is(err, postservice.ErrValidation) {
			failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to replace post media")
			return
		}
		payload := make([]gin.H, 0, len(out))
		for _, item := range out {
			row := gin.H{
				"media_asset_id": item.MediaAssetID.String(),
				"kind":           item.Kind,
				"sort_order":     item.SortOrder,
			}
			if item.Media != nil {
				row["media"] = mediaAssetJSON(*item.Media)
			}
			payload = append(payload, row)
		}
		success(c, nethttp.StatusOK, gin.H{
			"post_id": postID.String(),
			"items":   payload,
		})
	}
}

func categoryJSON(cat categorydomain.Category) gin.H {
	var parent any
	if cat.ParentID != nil {
		parent = cat.ParentID.String()
	}
	return gin.H{
		"id":          cat.ID.String(),
		"name":        cat.Name,
		"slug":        cat.Slug,
		"description": cat.Description,
		"parent_id":   parent,
		"created_at":  cat.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func listTagsHandler(tags *persistence.TagRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := tags.List(c.Request.Context())
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list tags")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, tag := range items {
			out = append(out, gin.H{
				"id":         tag.ID.String(),
				"name":       tag.Name,
				"slug":       tag.Slug,
				"created_at": tag.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		success(c, nethttp.StatusOK, out)
	}
}
