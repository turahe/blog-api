package handlers

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

type tagAPI interface {
	List(ctx context.Context) ([]tagdomain.Tag, error)
	Create(ctx context.Context, name, slug string) (tagdomain.Tag, error)
	Update(ctx context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error)
	Merge(ctx context.Context, sourceID, intoID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// listTagsHandler godoc
//
//	@Summary	List tags
//	@Tags		public
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Router		/api/v1/tags [get]
func listTagsHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := tags.List(c.Request.Context())
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Failed to list tags")
			return
		}

		out := make([]gin.H, 0, len(items))
		for _, tag := range items {
			out = append(out, responses.Tag(tag))
		}

		responses.Success(c, nethttp.StatusOK, out)
	}
}

// adminCreateTagHandler godoc
//
//	@Summary	Create tag
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.CreateTag	true	"tag"
//	@Success	201		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/tags [post]
func adminCreateTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.CreateTag
		if !requests.BindJSON(c, &req) {
			return
		}

		tag, err := tags.Create(c.Request.Context(), req.Name, req.Slug)
		if mapTagError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusCreated, responses.Tag(tag))
	}
}

// adminUpdateTagHandler godoc
//
//	@Summary	Update tag
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string				true	"tag UUID"
//	@Param		body	body		requests.UpdateTag	true	"patch fields"
//	@Success	200		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/tags/{param1} [patch]
func adminUpdateTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid tag id")
			return
		}

		var req requests.UpdateTag
		if !requests.BindJSON(c, &req) {
			return
		}

		tag, err := tags.Update(c.Request.Context(), id, req.Name, req.Slug)
		if mapTagError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.Tag(tag))
	}
}

// adminMergeTagHandler godoc
//
//	@Summary	Merge tag into another
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string				true	"source tag UUID"
//	@Param		body	body		requests.MergeTag	true	"target tag"
//	@Success	200		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/tags/{param1}/merge [post]
func adminMergeTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid tag id")
			return
		}

		var req requests.MergeTag
		if !requests.BindJSON(c, &req) {
			return
		}

		intoID, err := uuid.Parse(strings.TrimSpace(req.IntoID))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid into_id")
			return
		}

		if err := tags.Merge(c.Request.Context(), sourceID, intoID); mapTagError(c, err) {
			return
		}

		items, err := tags.List(c.Request.Context())
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Failed to load merged tag")
			return
		}

		for _, tag := range items {
			if tag.UUID == intoID {
				responses.Success(c, nethttp.StatusOK, responses.Tag(tag))
				return
			}
		}

		responses.Failure(c, nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Failed to load merged tag")
	}
}

// adminDeleteTagHandler godoc
//
//	@Summary	Delete tag
//	@Tags		admin
//	@Param		param1	path		string	true	"tag UUID"
//	@Success	200		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/tags/{param1} [delete]
func adminDeleteTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid tag id")
			return
		}

		if err := tags.Delete(c.Request.Context(), id); mapTagError(c, err) {
			return
		}

		responses.Success(c, nethttp.StatusOK, nil)
	}
}

var tagErrors = resourceErrors{
	service:    responses.ServiceTags,
	name:       "Tag",
	inUseCode:  "tag_in_use",
	validation: tagservice.ErrValidation,
	notFound:   tagdomain.ErrNotFound,
	conflict:   tagdomain.ErrConflict,
	inUse:      tagdomain.ErrInUse,
}

func mapTagError(c *gin.Context, err error) bool {
	return tagErrors.write(c, err)
}
