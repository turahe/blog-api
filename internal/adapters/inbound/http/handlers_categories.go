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
	Create(ctx context.Context, in categoryservice.CreateInput) (categorydomain.Category, error)
	Update(ctx context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Move(ctx context.Context, id uuid.UUID, parentID, beforeID *uuid.UUID) (categorydomain.Category, error)
}

type categoryCreateRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	ParentID    *string `json:"parent_id"`
	ImageID     *string `json:"image_id"`
	BeforeID    *string `json:"before_id"`
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
		success(c, nethttp.StatusOK, gin.H{"items": out})
	}
}

func getCategoryHandler(cats categoryAPI) gin.HandlerFunc {
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

func adminCreateCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req categoryCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		parentID, err := parseOptionalUUIDString(req.ParentID, "parent_id")
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		imageID, err := parseOptionalUUIDString(req.ImageID, "image_id")
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		beforeID, err := parseOptionalUUIDString(req.BeforeID, "before_id")
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		cat, err := cats.Create(c.Request.Context(), categoryservice.CreateInput{
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			ParentID:    parentID,
			ImageID:     imageID,
			BeforeID:    beforeID,
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
		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		if _, ok := raw["parent_id"]; ok {
			failure(c, nethttp.StatusBadRequest, "validation_error", "parent_id cannot be updated via PATCH; use move")
			return
		}
		in := categoryservice.UpdateInput{}
		if v, ok := raw["name"]; ok {
			var name string
			if err := json.Unmarshal(v, &name); err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid name")
				return
			}
			in.Name = &name
		}
		if v, ok := raw["slug"]; ok {
			var slug string
			if err := json.Unmarshal(v, &slug); err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid slug")
				return
			}
			in.Slug = &slug
		}
		if v, ok := raw["description"]; ok {
			if isJSONNull(v) {
				empty := ""
				in.Description = &empty
			} else {
				var desc string
				if err := json.Unmarshal(v, &desc); err != nil {
					failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid description")
					return
				}
				in.Description = &desc
			}
		}
		if v, ok := raw["image_id"]; ok {
			in.ImageIDProvided = true
			if isJSONNull(v) {
				in.ImageID = nil
			} else {
				var s string
				if err := json.Unmarshal(v, &s); err != nil {
					failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
					return
				}
				imageID, err := uuid.Parse(strings.TrimSpace(s))
				if err != nil {
					failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
					return
				}
				in.ImageID = &imageID
			}
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
		c.AbortWithStatus(nethttp.StatusNoContent)
	}
}

func adminMoveCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
			return
		}
		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		parentRaw, ok := raw["parent_id"]
		if !ok {
			failure(c, nethttp.StatusBadRequest, "validation_error", "parent_id required")
			return
		}
		var parentID *uuid.UUID
		if !isJSONNull(parentRaw) {
			var s string
			if err := json.Unmarshal(parentRaw, &s); err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			pid, err := uuid.Parse(strings.TrimSpace(s))
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			parentID = &pid
		}
		var beforeID *uuid.UUID
		if beforeRaw, ok := raw["before_id"]; ok && !isJSONNull(beforeRaw) {
			var s string
			if err := json.Unmarshal(beforeRaw, &s); err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid before_id")
				return
			}
			bid, err := uuid.Parse(strings.TrimSpace(s))
			if err != nil {
				failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid before_id")
				return
			}
			beforeID = &bid
		}
		cat, err := cats.Move(c.Request.Context(), id, parentID, beforeID)
		if mapCategoryError(c, err) {
			return
		}
		success(c, nethttp.StatusOK, categoryJSON(cat))
	}
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
	case errors.Is(err, categorydomain.ErrInUse):
		failure(c, nethttp.StatusConflict, "category_in_use", "Category in use")
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
		"lft":         cat.Lft,
		"rgt":         cat.Rgt,
		"depth":       cat.Depth,
		"sort_order":  cat.SortOrder,
		"created_at":  cat.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  cat.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func parseOptionalUUIDString(raw *string, field string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, errors.New("invalid " + field)
	}
	return &id, nil
}
