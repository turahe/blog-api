package http

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

type postAdminAPI interface {
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error)
}

type updatePostRequest struct {
	Title      *string          `json:"title"`
	Slug       *string          `json:"slug"`
	Excerpt    *string          `json:"excerpt"`
	Content    *string          `json:"content"`
	CategoryID *json.RawMessage `json:"category_id"`
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

func adminListPostsHandler(posts *postservice.PostService, roles roleLookup) gin.HandlerFunc {
	return adminListPostsHandlerWithDeps(posts, roles)
}

func adminListPostsHandlerWithDeps(posts postAdminAPI, roles roleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		authorID, err := postservice.ParseOptionalUUID(c.Query("author_id"))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid author_id")
			return
		}
		categoryID, err := postservice.ParseOptionalUUID(c.Query("category_id"))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
			return
		}

		filter := postdomain.AdminListFilter{
			Page:       page,
			PerPage:    perPage,
			Status:     c.Query("status"),
			AuthorID:   authorID,
			CategoryID: categoryID,
			Query:      c.Query("q"),
		}
		if !unrestricted {
			filter.ScopeAuthorID = &userID
		}

		result, err := posts.ListAdmin(c.Request.Context(), filter)
		if mapPostError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, postJSON(post))
		}
		successWithMeta(c, nethttp.StatusOK, items, &Meta{
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
		})
	}
}

func adminUpdatePostHandler(posts *postservice.PostService, roles roleLookup) gin.HandlerFunc {
	return adminUpdatePostHandlerWithDeps(posts, roles)
}

func adminUpdatePostHandlerWithDeps(posts postAdminAPI, roles roleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}

		var req updatePostRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
			return
		}

		in := postdomain.UpdateInput{
			Title:   req.Title,
			Slug:    req.Slug,
			Excerpt: req.Excerpt,
			Content: req.Content,
		}
		if req.CategoryID != nil {
			in.CategoryID.Present = true
			if isJSONNull(*req.CategoryID) {
				in.CategoryID.Value = nil
			} else {
				var raw string
				if err := json.Unmarshal(*req.CategoryID, &raw); err != nil {
					failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
					return
				}
				categoryID, err := uuid.Parse(strings.TrimSpace(raw))
				if err != nil {
					failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
					return
				}
				in.CategoryID.Value = &categoryID
			}
		}

		post, err := posts.Update(c.Request.Context(), postID, userID, unrestricted, in)
		if mapPostError(c, err) {
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

func isUnrestrictedEditor(roles []string) bool {
	for _, role := range roles {
		switch strings.TrimSpace(strings.ToLower(role)) {
		case "admin", "editor":
			return true
		}
	}
	return false
}

func resolveUnrestrictedEditor(ctx context.Context, roles roleLookup, userID uuid.UUID) (bool, error) {
	if roles == nil {
		return false, nil
	}
	names, err := roles.ListRoleNames(ctx, userID)
	if err != nil {
		return false, err
	}
	return isUnrestrictedEditor(names), nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func mapPostError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, postservice.ErrValidation):
		failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, postdomain.ErrNotFound):
		failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
	case errors.Is(err, postservice.ErrConflict):
		failure(c, nethttp.StatusConflict, "conflict", "Post conflict")
	default:
		failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to process post")
	}
	return true
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
