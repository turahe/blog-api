package handlers

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
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

func listCategoriesHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := cats.List(c.Request.Context())
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list categories")
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, item := range items {
			out = append(out, responses.Category(item))
		}
		responses.Success(c, nethttp.StatusOK, gin.H{"items": out})
	}
}

func getCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		item, err := cats.GetBySlug(c.Request.Context(), c.Param("param1"))
		if errors.Is(err, categorydomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusNotFound, "not_found", "Category not found")
			return
		}
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load category")
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Category(item))
	}
}

func adminCreateCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.CreateCategory
		if !requests.BindJSON(c, &req) {
			return
		}
		parentID, err := parseOptionalUUIDString(req.ParentID, "parent_id")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		imageID, err := parseOptionalUUIDString(req.ImageID, "image_id")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
			return
		}
		beforeID, err := parseOptionalUUIDString(req.BeforeID, "before_id")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", err.Error())
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
		responses.Success(c, nethttp.StatusCreated, responses.Category(cat))
	}
}

func adminUpdateCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
			return
		}
		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			requests.FailValidation(c, err)
			return
		}
		if _, ok := raw["parent_id"]; ok {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "parent_id cannot be updated via PATCH; use move")
			return
		}
		in := categoryservice.UpdateInput{}
		if v, ok := raw["name"]; ok {
			var name string
			if err := json.Unmarshal(v, &name); err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid name")
				return
			}
			in.Name = &name
		}
		if v, ok := raw["slug"]; ok {
			var slug string
			if err := json.Unmarshal(v, &slug); err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid slug")
				return
			}
			in.Slug = &slug
		}
		if v, ok := raw["description"]; ok {
			if requests.IsJSONNull(v) {
				empty := ""
				in.Description = &empty
			} else {
				var desc string
				if err := json.Unmarshal(v, &desc); err != nil {
					responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid description")
					return
				}
				in.Description = &desc
			}
		}
		if v, ok := raw["image_id"]; ok {
			in.ImageIDProvided = true
			if requests.IsJSONNull(v) {
				in.ImageID = nil
			} else {
				var s string
				if err := json.Unmarshal(v, &s); err != nil {
					responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
					return
				}
				imageID, err := uuid.Parse(strings.TrimSpace(s))
				if err != nil {
					responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid image_id")
					return
				}
				in.ImageID = &imageID
			}
		}
		cat, err := cats.Update(c.Request.Context(), id, in)
		if mapCategoryError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Category(cat))
	}
}

func adminDeleteCategoryHandler(cats categoryAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
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
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category id")
			return
		}
		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			requests.FailValidation(c, err)
			return
		}
		parentRaw, ok := raw["parent_id"]
		if !ok {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "parent_id required")
			return
		}
		var parentID *uuid.UUID
		if !requests.IsJSONNull(parentRaw) {
			var s string
			if err := json.Unmarshal(parentRaw, &s); err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			pid, err := uuid.Parse(strings.TrimSpace(s))
			if err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid parent_id")
				return
			}
			parentID = &pid
		}
		var beforeID *uuid.UUID
		if beforeRaw, ok := raw["before_id"]; ok && !requests.IsJSONNull(beforeRaw) {
			var s string
			if err := json.Unmarshal(beforeRaw, &s); err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid before_id")
				return
			}
			bid, err := uuid.Parse(strings.TrimSpace(s))
			if err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid before_id")
				return
			}
			beforeID = &bid
		}
		cat, err := cats.Move(c.Request.Context(), id, parentID, beforeID)
		if mapCategoryError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Category(cat))
	}
}

func mapCategoryError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, categoryservice.ErrValidation):
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.ServiceCategories, responses.CaseValidation, "validation_error", err.Error(), nil)
	case errors.Is(err, categorydomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.ServiceCategories, responses.CaseNotFound, "not_found", "Category not found", nil)
	case errors.Is(err, categorydomain.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceCategories, responses.CaseConflict, "conflict", "Category conflict", nil)
	case errors.Is(err, categorydomain.ErrInUse):
		responses.FailureFor(c, nethttp.StatusConflict, responses.ServiceCategories, responses.CaseConflict, "category_in_use", "Category in use", nil)
	default:
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.ServiceCategories, responses.CaseInternalError, "internal_error", "Failed to process category", nil)
	}
	return true
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
