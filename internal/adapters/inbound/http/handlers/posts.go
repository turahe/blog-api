package handlers

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

type postAdminAPI interface {
	ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error)
	Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error)
}

// adminCreatePostHandler godoc
//
//	@Summary	Create draft post
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.CreatePost	true	"draft post"
//	@Success	201		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts [post]
func adminCreatePostHandler(posts postAdminAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		var req requests.CreatePost
		if !requests.BindJSON(c, &req) {
			return
		}
		var categoryID *uuid.UUID
		if req.CategoryID != nil && strings.TrimSpace(*req.CategoryID) != "" {
			id, err := uuid.Parse(strings.TrimSpace(*req.CategoryID))
			if err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
				return
			}
			categoryID = &id
		}
		post, tags, err := posts.CreateDraft(
			c.Request.Context(),
			userID,
			req.Title,
			req.Slug,
			req.Excerpt,
			req.Content,
			categoryID,
			req.Tags,
		)
		if mapPostError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusCreated, responses.PostWithTags(post, tags))
	}
}

// adminPublishPostHandler godoc
//
//	@Summary	Publish post
//	@Tags		admin
//	@Produce	json
//	@Param		param1	path		string	true	"post UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts/{param1}/publish [post]
func adminPublishPostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}
		post, err := posts.Publish(c.Request.Context(), id)
		if errors.Is(err, postdomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to publish post")
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Post(post))
	}
}

// adminListPostsHandler godoc
//
//	@Summary	List admin posts
//	@Tags		admin
//	@Produce	json
//	@Param		page		query		int		false	"page"		default(1)
//	@Param		per_page	query		int		false	"per page"	default(20)
//	@Param		status		query		string	false	"status filter"
//	@Param		author_id	query		string	false	"author UUID"
//	@Param		category_id	query		string	false	"category UUID"
//	@Param		q			query		string	false	"search"
//	@Success	200			{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts [get]
func adminListPostsHandler(posts *postservice.PostService, roles RoleLookup) gin.HandlerFunc {
	return adminListPostsHandlerWithDeps(posts, roles)
}

func adminListPostsHandlerWithDeps(posts postAdminAPI, roles RoleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		authorID, err := postservice.ParseOptionalUUID(c.Query("author_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid author_id")
			return
		}
		categoryID, err := postservice.ParseOptionalUUID(c.Query("category_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
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
			items = append(items, responses.Post(post))
		}
		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServicePosts,
			Data:    items,
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
		})
	}
}

// adminUpdatePostHandler godoc
//
//	@Summary	Update post
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string				true	"post UUID"
//	@Param		body	body		requests.UpdatePost	true	"patch fields"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts/{param1} [patch]
func adminUpdatePostHandler(posts *postservice.PostService, roles RoleLookup) gin.HandlerFunc {
	return adminUpdatePostHandlerWithDeps(posts, roles)
}

func adminUpdatePostHandlerWithDeps(posts postAdminAPI, roles RoleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}

		var req requests.UpdatePost
		if !requests.BindJSON(c, &req) {
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to resolve roles")
			return
		}

		in := postdomain.UpdateInput{
			Title:   req.Title,
			Slug:    req.Slug,
			Excerpt: req.Excerpt,
			Content: req.Content,
			Tags:    req.Tags,
		}
		if req.CategoryID != nil {
			in.CategoryID.Present = true
			categoryID, ok := parseNullableUUID(c, *req.CategoryID, "category_id")
			if !ok {
				return
			}
			in.CategoryID.Value = categoryID
		}

		post, tags, err := posts.Update(c.Request.Context(), postID, userID, unrestricted, in)
		if mapPostError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.PostWithTags(post, tags))
	}
}

// adminReplacePostMediaHandler godoc
//
//	@Summary	Replace post media
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string						true	"post UUID"
//	@Param		body	body		requests.ReplacePostMedia	true	"media items"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts/{param1}/media [patch]
func adminReplacePostMediaHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid post id")
			return
		}
		var req requests.ReplacePostMedia
		if !requests.BindJSON(c, &req) {
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
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid media_asset_id")
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
			responses.Failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if errors.Is(err, postservice.ErrValidation) {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to replace post media")
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
				row["media"] = responses.MediaAsset(*item.Media)
			}
			payload = append(payload, row)
		}
		responses.Success(c, nethttp.StatusOK, gin.H{
			"post_id": postID.String(),
			"items":   payload,
		})
	}
}

// parseNullableUUID returns (nil, true) for JSON null, (id, true) for a UUID string,
// or (nil, false) after writing a validation failure.
func parseNullableUUID(c *gin.Context, raw json.RawMessage, field string) (*uuid.UUID, bool) {
	if requests.IsJSONNull(raw) {
		return nil, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid "+field)
		return nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid "+field)
		return nil, false
	}
	return &id, true
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

func resolveUnrestrictedEditor(ctx context.Context, roles RoleLookup, userID uuid.UUID) (bool, error) {
	if roles == nil {
		return false, nil
	}
	names, err := roles.ListRoleNames(ctx, userID)
	if err != nil {
		return false, err
	}
	return isUnrestrictedEditor(names), nil
}

func mapPostError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, postservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseValidation,
			Code:    "validation_error",
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, tagservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseValidation,
			Code:    "validation_error",
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, postdomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseNotFound,
			Code:    "not_found",
			Message: "Post not found",
			Details: nil,
		})
	case errors.Is(err, tagdomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseNotFound,
			Code:    "not_found",
			Message: "Tag not found",
			Details: nil,
		})
	case errors.Is(err, postservice.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseConflict,
			Code:    "conflict",
			Message: "Post conflict",
			Details: nil,
		})
	case errors.Is(err, tagdomain.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseConflict,
			Code:    "conflict",
			Message: "Tag conflict",
			Details: nil,
		})
	default:
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseInternalError,
			Code:    "internal_error",
			Message: "Failed to process post",
			Details: nil,
		})
	}
	return true
}
