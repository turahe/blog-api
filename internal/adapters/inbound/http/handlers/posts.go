package handlers

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/shared/pagination"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

//nolint:dupl // the test fake's function fields intentionally mirror these signatures
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
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
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
				responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid categoryId")
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

// adminListPostsHandler godoc
//
//	@Summary		List admin posts
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			admin
//	@Produce		json
//	@Param			page			query		int		false	"page (legacy offset mode)"						default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"					default(20)
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(false)
//	@Param			status			query		string	false	"status filter"
//	@Param			authorId		query		string	false	"author UUID"
//	@Param			categoryId		query		string	false	"category UUID"
//	@Param			q				query		string	false	"search"
//	@Param			trashed			query		bool	false	"list only soft-deleted posts"	default(false)
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		401				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts [get]
func adminListPostsHandler(posts *postservice.PostService, roles RoleLookup) gin.HandlerFunc {
	return adminListPostsHandlerWithDeps(posts, roles)
}

// postAdminCfg declares the pagination contract used by the admin post list.
// Duplicated here (from repo) rather than exported from the persistence
// package to keep HTTP wiring free of repository-specific imports.
var postAdminCfg = pagination.CursorConfig{
	Kind: "posts_admin",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

func adminListPostsHandlerWithDeps(posts postAdminAPI, roles RoleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		pr, err := pagination.ParseRequest(c, postAdminCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		authorID, err := postservice.ParseOptionalUUID(c.Query("authorId"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid authorId")
			return
		}

		categoryID, err := postservice.ParseOptionalUUID(c.Query("categoryId"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid categoryId")
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			responses.Internal(c, err, "Failed to resolve roles")
			return
		}

		trashed, err := parseBoolQuery(c, "trashed", false)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid trashed")
			return
		}

		filter := postdomain.AdminListFilter{
			Status:       c.Query("status"),
			AuthorUUID:   authorID,
			CategoryUUID: categoryID,
			Query:        c.Query("q"),
			Trashed:      trashed,
		}
		filter.PageRequest = pr
		if !unrestricted {
			filter.ScopeAuthorUUID = &userID
		}

		result, err := posts.ListAdmin(c.Request.Context(), filter)
		if mapPostError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, responses.Post(post))
		}

		responses.SuccessPaginatedResult[postdomain.Post](c, nethttp.StatusOK, responses.CursorPageOpts[postdomain.Post]{
			Service: responses.ServicePosts,
			Result:  result,
			Data:    items,
		})
	}
}

// parseBoolQuery returns the boolean value of query param name or def when empty.
func parseBoolQuery(c *gin.Context, name string, def bool) (bool, error) {
	v := strings.TrimSpace(c.Query(name))
	if v == "" {
		return def, nil
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return def, errors.New("invalid bool")
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
//	@Failure	409		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/posts/{param1} [patch]
func adminUpdatePostHandler(posts *postservice.PostService, roles RoleLookup) gin.HandlerFunc {
	return adminUpdatePostHandlerWithDeps(posts, roles)
}

func adminUpdatePostHandlerWithDeps(posts postAdminAPI, roles RoleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Authentication required")
			return
		}

		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid post id")
			return
		}

		var req requests.UpdatePost
		if !requests.BindJSON(c, &req) {
			return
		}

		unrestricted, err := resolveUnrestrictedEditor(c.Request.Context(), roles, userID)
		if err != nil {
			responses.Internal(c, err, "Failed to resolve roles")
			return
		}

		in := postdomain.UpdateInput{
			Title:   req.Title,
			Slug:    req.Slug,
			Excerpt: req.Excerpt,
			Content: req.Content,
			Tags:    req.Tags,
		}
		if req.CommentPolicy != nil {
			policy := postdomain.CommentPolicy(*req.CommentPolicy)
			in.CommentPolicy = &policy
		}

		if len(req.CategoryID) > 0 {
			in.CategoryUUID.Present = true

			categoryID, ok := parseNullableUUID(c, req.CategoryID, "categoryId")
			if !ok {
				return
			}

			in.CategoryUUID.Value = categoryID
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
func adminReplacePostMediaHandler(posts *postservice.PostService, media mediaports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		postID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid post id")
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
				responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid mediaAssetId")
				return
			}

			items = append(items, mediadomain.PostMediaItem{
				MediaAssetUUID: mediaID,
				Kind:           item.Kind,
				SortOrder:      item.SortOrder,
			})
		}

		out, err := posts.ReplaceMedia(c.Request.Context(), postID, items, enforce)
		if errors.Is(err, postdomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Post not found")
			return
		}

		if errors.Is(err, postservice.ErrValidation) {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
			return
		}

		if err != nil {
			responses.Internal(c, err, "Failed to replace post media")
			return
		}

		payload, ok := postMediaPayload(c, media, out)
		if !ok {
			return
		}

		responses.Success(c, nethttp.StatusOK, gin.H{
			"postId": postID.String(),
			"items":  payload,
		})
	}
}

// postMediaPayload serializes post media rows with their assets and preset URLs. It writes the
// error and returns false when the presets cannot be read.
func postMediaPayload(c *gin.Context, media mediaports.Service, items []mediadomain.PostMediaItem) ([]gin.H, bool) {
	assets := make([]mediadomain.MediaAsset, 0, len(items))
	for _, item := range items {
		if item.Media != nil {
			assets = append(assets, *item.Media)
		}
	}

	rendered, ok := renderMedia(c, media, assets...)
	if !ok {
		return nil, false
	}

	payload := make([]gin.H, 0, len(items))
	for _, item := range items {
		row := gin.H{
			"mediaAssetId": item.MediaAssetUUID.String(),
			"kind":         item.Kind,
			"sortOrder":    item.SortOrder,
		}
		if item.Media != nil {
			row["media"], rendered = rendered[0], rendered[1:]
		}

		payload = append(payload, row)
	}

	return payload, true
}

// parseNullableUUID returns (nil, true) for JSON null, (id, true) for a UUID string,
// or (nil, false) after writing a validation failure.
func parseNullableUUID(c *gin.Context, raw json.RawMessage, field string) (*uuid.UUID, bool) {
	if requests.IsJSONNull(raw) {
		return nil, true
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid "+field)
		return nil, false
	}

	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid "+field)
		return nil, false
	}

	return &id, true
}

func isUnrestrictedEditor(roles []string) bool {
	for _, role := range roles {
		switch strings.TrimSpace(strings.ToLower(role)) {
		case roleAdmin, roleEditor:
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
			Code:    responses.ErrorCodeValidation,
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, tagservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseValidation,
			Code:    responses.ErrorCodeValidation,
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, postdomain.ErrRevisionNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseNotFound,
			Code:    responses.ErrorCodeNotFound,
			Message: "Revision not found",
			Details: nil,
		})
	case errors.Is(err, postdomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseNotFound,
			Code:    responses.ErrorCodeNotFound,
			Message: "Post not found",
			Details: nil,
		})
	case errors.Is(err, tagdomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseNotFound,
			Code:    responses.ErrorCodeNotFound,
			Message: "Tag not found",
			Details: nil,
		})
	case errors.Is(err, postdomain.ErrInvalidTransition):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseConflict,
			Code:    "post.invalid_transition",
			Message: strings.TrimPrefix(err.Error(), postdomain.ErrConflict.Error()+": "),
			Details: nil,
		})
	case errors.Is(err, postdomain.ErrStaleVersion):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseConflict,
			Code:    "post.version_conflict",
			Message: "Post was modified by another request; reload and retry",
			Details: nil,
		})
	case errors.Is(err, postservice.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseConflict,
			Code:    responses.ErrorCodeConflict,
			Message: "Post conflict",
			Details: nil,
		})
	case errors.Is(err, tagdomain.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceTags,
			Case:    responses.CaseConflict,
			Code:    responses.ErrorCodeConflict,
			Message: "Tag conflict",
			Details: nil,
		})
	default:
		responses.RecordError(c, err)
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseInternalError,
			Code:    responses.ErrorCodeInternal,
			Message: "Failed to process post",
			Details: nil,
		})
	}

	return true
}
