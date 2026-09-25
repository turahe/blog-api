package handlers

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type postLifecycleAPI interface {
	Publish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	PublishBy(ctx context.Context, actorID, id uuid.UUID) (postdomain.Post, error)
	Unpublish(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Archive(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) (postdomain.Post, error)
}

// adminPublishPostHandler godoc
//
//	@Summary		Publish post
//	@Description	Allowed from draft, scheduled, or archived; sets publishedAt to now. Returns 409 post.invalid_transition for a post that is already published.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/publish [post]
func adminPublishPostHandler(posts postLifecycleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		publish := posts.Publish
		if actorID, ok := middleware.CurrentUserID(c); ok {
			publish = func(ctx context.Context, id uuid.UUID) (postdomain.Post, error) {
				return posts.PublishBy(ctx, actorID, id)
			}
		}

		postTransitionHandler(publish)(c)
	}
}

// adminUnpublishPostHandler godoc
//
//	@Summary		Unpublish post
//	@Description	Returns a published or scheduled post to draft and clears publishedAt.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/unpublish [post]
func adminUnpublishPostHandler(posts postLifecycleAPI) gin.HandlerFunc {
	return postTransitionHandler(posts.Unpublish)
}

// adminArchivePostHandler godoc
//
//	@Summary		Archive post
//	@Description	Hides a draft, scheduled, or published post from public reads; publishedAt is kept. Publish again to bring it back.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/archive [post]
func adminArchivePostHandler(posts postLifecycleAPI) gin.HandlerFunc {
	return postTransitionHandler(posts.Archive)
}

// adminRestorePostHandler godoc
//
//	@Summary		Restore deleted post
//	@Description	Brings a soft-deleted post back as a draft. If a live post took its slug meanwhile, the slug gets the lowest free numeric suffix (-2, -3, …).
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/restore [post]
func adminRestorePostHandler(posts postLifecycleAPI) gin.HandlerFunc {
	return postTransitionHandler(posts.Restore)
}

// adminDeletePostHandler godoc
//
//	@Summary		Delete post
//	@Description	Soft delete: the post leaves public and admin reads and releases its slug. List with trashed=true and restore to undo.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1} [delete]
func adminDeletePostHandler(posts postLifecycleAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := postPathID(c)
		if !ok {
			return
		}

		if err := posts.Delete(c.Request.Context(), id); mapPostError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, gin.H{"id": id.String(), "deleted": true})
	}
}

func postTransitionHandler(apply func(context.Context, uuid.UUID) (postdomain.Post, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := postPathID(c)
		if !ok {
			return
		}

		post, err := apply(c.Request.Context(), id)
		if mapPostError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.Post(post))
	}
}

func postPathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid post id")
		return uuid.Nil, false
	}

	return id, true
}
