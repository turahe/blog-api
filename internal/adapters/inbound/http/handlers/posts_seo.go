package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type postSEOAPI interface {
	GetSEO(ctx context.Context, postID, viewerID uuid.UUID, unrestricted bool) (postservice.SEOView, error)
	UpdateSEO(ctx context.Context, postID, actorID uuid.UUID, unrestricted, slugAllowed bool, patch postdomain.SEOPatch) (postservice.SEOView, error)
	PreviewSEO(ctx context.Context, postID, viewerID uuid.UUID, unrestricted bool, draft postservice.SEODraft) (postdomain.SEOPreview, error)
	SEOMeta(ctx context.Context, slug string) (postdomain.SEOMeta, error)
}

// seoAccess resolves, per request, whether the caller may edit any post and change slugs.
type seoAccess struct {
	unrestricted func(c *gin.Context, userID uuid.UUID) (bool, error)
	slugAllowed  func(c *gin.Context) bool
}

// postSEOAccess edits any post for admins and editors, and allows slug changes to
// holders of post.slug.edit.
func postSEOAccess(deps Deps) seoAccess {
	return seoAccess{
		unrestricted: func(c *gin.Context, userID uuid.UUID) (bool, error) {
			return resolveUnrestrictedEditor(c.Request.Context(), deps.Roles, userID)
		},
		slugAllowed: func(c *gin.Context) bool { return holds(c, deps, permPostSlugEdit, authorRoles) },
	}
}

// adminGetPostSEOHandler godoc
//
//	@Summary		Get post SEO
//	@Description	The post's stored SEO overrides (empty fields fall back to derived values), its slug, and advisory warnings such as a title longer than 60 characters. Authors see only their own posts.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/seo [get]
func adminGetPostSEOHandler(posts postSEOAPI, access seoAccess) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, postID, unrestricted, ok := seoTarget(c, access)
		if !ok {
			return
		}

		view, err := posts.GetSEO(c.Request.Context(), postID, userID, unrestricted)
		if mapSEOError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, responses.PostSEO(view))
	}
}

// adminUpdatePostSEOHandler godoc
//
//	@Summary		Update post SEO
//	@Description	Partial update: omitted fields stay unchanged, "" clears a text field, and null clears an image. Text is trimmed and whitespace-collapsed; markup is rejected. Canonical and og:url hosts must be the site host or in seo.canonical_allowed_hosts. Changing slug needs post.slug.edit. Every invalid field is listed in error.details with a code (too_long, invalid_format, markup_not_allowed, host_not_allowed, not_found, not_image, slug_taken, slug_reserved).
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string					true	"post UUID"
//	@Param			body	body		requests.UpdatePostSEO	true	"SEO fields to change"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/seo [put]
func adminUpdatePostSEOHandler(posts postSEOAPI, access seoAccess) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, postID, unrestricted, ok := seoTarget(c, access)
		if !ok {
			return
		}

		var req requests.UpdatePostSEO
		if !requests.BindJSON(c, &req) {
			return
		}

		view, err := posts.UpdateSEO(c.Request.Context(), postID, userID, unrestricted, access.slugAllowed(c), req.Patch())
		if mapSEOError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, responses.PostSEO(view))
	}
}

// adminPreviewPostSEOHandler godoc
//
//	@Summary		Preview post SEO
//	@Description	Renders the search snippet, Open Graph card, and Twitter card the post would get with the submitted SEO fields and optional unsaved title and excerpt. Nothing is saved.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string					true	"post UUID"
//	@Param			body	body		requests.PreviewPostSEO	false	"draft SEO fields"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/seo/preview [post]
func adminPreviewPostSEOHandler(posts postSEOAPI, access seoAccess) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, postID, unrestricted, ok := seoTarget(c, access)
		if !ok {
			return
		}

		var req requests.PreviewPostSEO
		if c.Request.ContentLength != 0 && !requests.BindJSON(c, &req) {
			return
		}

		preview, err := posts.PreviewSEO(c.Request.Context(), postID, userID, unrestricted, postservice.SEODraft{
			Patch: req.Patch(), Title: req.Title, Excerpt: req.Excerpt,
		})
		if mapSEOError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, responses.SEOPreview(preview))
	}
}

// publicPostSEOMetaHandler godoc
//
//	@Summary		Get post SEO meta
//	@Description	Rendered meta for a published post: title, description, canonical URL, robots, Open Graph, and Twitter card values with every fallback applied. Sends X-Robots-Tag when the post is noindex or nofollow.
//	@Tags			public
//	@Produce		json
//	@Param			param1	path		string	true	"post slug"
//	@Success		200		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Router			/api/v1/posts/{param1}/seo-meta [get]
func publicPostSEOMetaHandler(posts postSEOAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		meta, err := posts.SEOMeta(c.Request.Context(), strings.TrimSpace(c.Param("param1")))
		if mapSEOError(c, err) {
			return
		}

		if meta.Robots != "" && meta.Robots != "index,follow" {
			c.Header("X-Robots-Tag", meta.Robots)
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, responses.SEOMeta(meta))
	}
}

func seoTarget(c *gin.Context, access seoAccess) (uuid.UUID, uuid.UUID, bool, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Unauthorized")
		return uuid.Nil, uuid.Nil, false, false
	}

	postID, ok := postPathID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false, false
	}

	unrestricted, err := access.unrestricted(c, userID)
	if err != nil {
		responses.Internal(c, err, "Failed to resolve roles")
		return uuid.Nil, uuid.Nil, false, false
	}

	return userID, postID, unrestricted, true
}

// mapSEOError reports field violations as 422 and a forbidden slug change as 403, then
// falls back to mapPostError.
func mapSEOError(c *gin.Context, err error) bool {
	var invalid *postdomain.SEOValidationError

	switch {
	case errors.As(err, &invalid):
		responses.FailureFor(c, nethttp.StatusUnprocessableEntity, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseUnprocessable,
			Code:    responses.ErrorCodeValidation,
			Message: "Invalid SEO fields",
			Details: responses.FieldViolations(invalid.Violations),
		})

		return true
	case errors.Is(err, postdomain.ErrSlugEditForbidden):
		responses.FailureFor(c, nethttp.StatusForbidden, responses.FailureOpts{
			Service: responses.ServicePosts,
			Case:    responses.CaseForbidden,
			Code:    responses.ErrorCodeForbidden,
			Message: "Changing the slug requires post.slug.edit",
			Details: nil,
		})

		return true
	default:
		return mapPostError(c, err)
	}
}
