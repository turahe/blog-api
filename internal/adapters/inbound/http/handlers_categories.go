package http

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
)

type categoryAPI interface {
	List(ctx context.Context) ([]categorydomain.Category, error)
	GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error)
	Create(ctx context.Context, in categorydomain.CreateInput) (categorydomain.Category, error)
	Update(ctx context.Context, id uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error
}

type categoryCreateRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id"`
	ImageID     *string `json:"image_id"`
}

type categoryUpdateRequest struct {
	Name        *string         `json:"name"`
	Slug        *string         `json:"slug"`
	Description *string         `json:"description"`
	ParentID    json.RawMessage `json:"parent_id"`
	ImageID     json.RawMessage `json:"image_id"`
}

type categoryReorderRequest struct {
	ParentID   json.RawMessage `json:"parent_id"`
	OrderedIDs []string        `json:"ordered_ids"`
}

func listCategoriesHandler(cats categoryAPI) gin.HandlerFunc {
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

func getCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		item, err := cats.GetBySlug(c.Request.Context(), c.Param("param1"))
		if mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, categoryJSON(item))
	}
}

func adminCreateCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req categoryCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		parentID, err := parseOptionalUUIDPtr(req.ParentID)
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
			return
		}
		imageID, err := parseOptionalUUIDPtr(req.ImageID)
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
			return
		}
		cat, err := cats.Create(c.Request.Context(), categorydomain.CreateInput{
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			ParentID:    parentID,
			ImageID:     imageID,
		})
		if mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusCreated, categoryJSON(cat))
	}
}

func adminUpdateCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
			return
		}
		var req categoryUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		in := categorydomain.UpdateInput{
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
		}
		if len(req.ParentID) > 0 {
			opt, err := parseOptionalUUIDRaw(req.ParentID)
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			in.ParentID = opt
		}
		if len(req.ImageID) > 0 {
			opt, err := parseOptionalUUIDRaw(req.ImageID)
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
				return
			}
			in.ImageID = opt
		}
		cat, err := cats.Update(c.Request.Context(), id, in)
		if mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, categoryJSON(cat))
	}
}

func adminDeleteCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
			return
		}
		if err := cats.Delete(c.Request.Context(), id); mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, nil)
	}
}

func adminReorderCategoriesHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req categoryReorderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		var parentID *uuid.UUID
		if len(req.ParentID) > 0 {
			opt, err := parseOptionalUUIDRaw(req.ParentID)
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			parentID = opt.Value
		}
		ordered := make([]uuid.UUID, 0, len(req.OrderedIDs))
		for _, raw := range req.OrderedIDs {
			id, err := uuid.Parse(strings.TrimSpace(raw))
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid ordered_ids")
				return
			}
			ordered = append(ordered, id)
		}
		if err := cats.Reorder(c.Request.Context(), parentID, ordered); mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, nil)
	}
}

func parseOptionalUUIDPtr(raw *string) (*uuid.UUID, error) {
	if raw == nil {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseOptionalUUIDRaw(raw json.RawMessage) (categorydomain.OptionalUUID, error) {
	if isJSONNull(raw) {
		return categorydomain.OptionalUUID{Present: true, Value: nil}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return categorydomain.OptionalUUID{}, err
	}
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return categorydomain.OptionalUUID{}, err
	}
	return categorydomain.OptionalUUID{Present: true, Value: &id}, nil
}

func mapCategoryError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, categoryservice.ErrValidation):
		failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, categorydomain.ErrNotFound):
		failure(c, nethttp.StatusNotFound, "not_found", "Category not found")
	case errors.Is(err, categorydomain.ErrConflict):
		failure(c, nethttp.StatusConflict, "conflict", "Category conflict")
	case errors.Is(err, categorydomain.ErrHasChildren):
		failure(c, nethttp.StatusConflict, "category_has_children", "Category has children")
	default:
		failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to process category")
	}
	return true
}

func categoryJSON(cat categorydomain.Category) gin.H {
	var parent any
	if cat.ParentID != nil {
		parent = cat.ParentID.String()
	}
	var image any
	if cat.ImageID != nil {
		image = cat.ImageID.String()
	}
	return gin.H{
		"id":          cat.ID.String(),
		"name":        cat.Name,
		"slug":        cat.Slug,
		"description": cat.Description,
		"parent_id":   parent,
		"image_id":    image,
		"sort_order":  cat.SortOrder,
		"created_at":  cat.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
