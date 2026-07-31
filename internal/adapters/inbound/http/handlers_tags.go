package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

type tagCreateRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type tagUpdateRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

type tagMergeRequest struct {
	IntoID string `json:"into_id"`
}

func listTagsHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := tags.List(c.Request.Context())
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list tags")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, tag := range items {
			out = append(out, tagJSON(tag))
		}
		success(c, nethttp.StatusOK, out)
	}
}

func adminCreateTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req tagCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		tag, err := tags.Create(c.Request.Context(), req.Name, req.Slug)
		if mapTagError(c, err) {
			return
		}
		success(c, nethttp.StatusCreated, tagJSON(tag))
	}
}

func adminUpdateTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
			return
		}
		var req tagUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		tag, err := tags.Update(c.Request.Context(), id, req.Name, req.Slug)
		if mapTagError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, tagJSON(tag))
	}
}

func adminMergeTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
			return
		}
		var req tagMergeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		intoID, err := uuid.Parse(strings.TrimSpace(req.IntoID))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid into_id")
			return
		}
		if err := tags.Merge(c.Request.Context(), sourceID, intoID); mapTagError(c, err) {
			return
		}
		items, err := tags.List(c.Request.Context())
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load merged tag")
			return
		}
		for _, tag := range items {
			if tag.ID == intoID {
				success(c, nethttp.StatusOK, tagJSON(tag))
				return
			}
		}
		failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load merged tag")
	}
}

func adminDeleteTagHandler(tags tagAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag id")
			return
		}
		if err := tags.Delete(c.Request.Context(), id); mapTagError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, nil)
	}
}

func mapTagError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, tagservice.ErrValidation):
		failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, tagdomain.ErrNotFound):
		failure(c, nethttp.StatusNotFound, "not_found", "Tag not found")
	case errors.Is(err, tagdomain.ErrConflict):
		failure(c, nethttp.StatusConflict, "conflict", "Tag conflict")
	case errors.Is(err, tagdomain.ErrInUse):
		failure(c, nethttp.StatusConflict, "tag_in_use", "Tag in use")
	default:
		failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to process tag")
	}
	return true
}

func tagJSON(tag tagdomain.Tag) gin.H {
	return gin.H{
		"id":         tag.ID.String(),
		"name":       tag.Name,
		"slug":       tag.Slug,
		"created_at": tag.CreatedAt.UTC().Format(time.RFC3339),
	}
}
