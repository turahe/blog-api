package handlers

import (
	"context"
	"errors"
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

func listTagsHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := tags.List(c.Request.Context())
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list tags")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, tag := range items {
			out = append(out, responses.Tag(tag))
		}
		responses.Success(c, nethttp.StatusOK, out)
	}
}

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

func adminUpdateTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
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

func adminMergeTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
			return
		}
		var req requests.MergeTag
		if !requests.BindJSON(c, &req) {
			return
		}
		intoID, err := uuid.Parse(strings.TrimSpace(req.IntoID))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid into_id")
			return
		}
		if err := tags.Merge(c.Request.Context(), sourceID, intoID); mapTagError(c, err) {
			return
		}
		items, err := tags.List(c.Request.Context())
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load merged tag")
			return
		}
		for _, tag := range items {
			if tag.ID == intoID {
				responses.Success(c, nethttp.StatusOK, responses.Tag(tag))
				return
			}
		}
		responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load merged tag")
	}
}

func adminDeleteTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
			return
		}
		if err := tags.Delete(c.Request.Context(), id); mapTagError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusOK, nil)
	}
}

func mapTagError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, tagservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.ServiceTags, responses.CaseValidation, "validation_error", err.Error(), nil)
	case errors.Is(err, tagdomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.ServiceTags, responses.CaseNotFound, "not_found", "Tag not found", nil)
	case errors.Is(err, tagdomain.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceTags, responses.CaseConflict, "conflict", "Tag conflict", nil)
	case errors.Is(err, tagdomain.ErrInUse):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceTags, responses.CaseConflict, "tag_in_use", "Tag in use", nil)
	default:
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.ServiceTags, responses.CaseInternalError, "internal_error", "Failed to process tag", nil)
	}
	return true
}
