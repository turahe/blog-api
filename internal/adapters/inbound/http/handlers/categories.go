package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// listCategoriesHandler godoc
//
//	@Summary	List categories
//	@Tags		public
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Router		/api/v1/categories [get]
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

// getCategoryHandler godoc
//
//	@Summary	Get category by slug
//	@Tags		public
//	@Produce	json
//	@Param		param1	path		string	true	"category slug"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Router		/api/v1/categories/{param1} [get]
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

// adminCreateCategoryHandler godoc
//
//	@Summary	Create category
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		requests.CreateCategory	true	"category"
//	@Success	201		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/categories [post]
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

// adminUpdateCategoryHandler godoc
//
//	@Summary	Update category
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string	true	"category UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/categories/{param1} [patch]
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
			desc, ok := parseNullableString(c, v, "description")
			if !ok {
				return
			}
			if desc == nil {
				empty := ""
				in.Description = &empty
			} else {
				in.Description = desc
			}
		}
		if v, ok := raw["image_id"]; ok {
			in.ImageIDProvided = true
			imageID, ok := parseNullableUUIDField(c, v, "image_id")
			if !ok {
				return
			}
			in.ImageID = imageID
		}
		cat, err := cats.Update(c.Request.Context(), id, in)
		if mapCategoryError(c, err) {
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Category(cat))
	}
}

// adminDeleteCategoryHandler godoc
//
//	@Summary	Delete category
//	@Tags		admin
//	@Param		param1	path	string	true	"category UUID"
//	@Success	204		"No content"
//	@Failure	404		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/categories/{param1} [delete]
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

// adminMoveCategoryHandler godoc
//
//	@Summary	Move category in tree
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		param1	path		string	true	"category UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	400		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/categories/{param1}/move [post]
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
		parentID, ok := parseNullableUUIDField(c, parentRaw, "parent_id")
		if !ok {
			return
		}
		var beforeID *uuid.UUID
		if beforeRaw, hasBefore := raw["before_id"]; hasBefore {
			beforeID, ok = parseNullableUUIDField(c, beforeRaw, "before_id")
			if !ok {
				return
			}
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
		responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
			Service: responses.ServiceCategories,
			Case:    responses.CaseValidation,
			Code:    "validation_error",
			Message: err.Error(),
			Details: nil,
		})
	case errors.Is(err, categorydomain.ErrNotFound):
		responses.FailureFor(c, nethttp.StatusNotFound, responses.FailureOpts{
			Service: responses.ServiceCategories,
			Case:    responses.CaseNotFound,
			Code:    "not_found",
			Message: "Category not found",
			Details: nil,
		})
	case errors.Is(err, categorydomain.ErrConflict):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceCategories,
			Case:    responses.CaseConflict,
			Code:    "conflict",
			Message: "Category conflict",
			Details: nil,
		})
	case errors.Is(err, categorydomain.ErrInUse):
		responses.FailureFor(c, nethttp.StatusConflict, responses.FailureOpts{
			Service: responses.ServiceCategories,
			Case:    responses.CaseConflict,
			Code:    "category_in_use",
			Message: "Category in use",
			Details: nil,
		})
	default:
		responses.FailureFor(c, nethttp.StatusInternalServerError, responses.FailureOpts{
			Service: responses.ServiceCategories,
			Case:    responses.CaseInternalError,
			Code:    "internal_error",
			Message: "Failed to process category",
			Details: nil,
		})
	}
	return true
}

func parseOptionalUUIDString(raw *string, field string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, fmt.Errorf("invalid %q", field)
	}
	return &id, nil
}

func parseNullableString(c *gin.Context, raw json.RawMessage, field string) (*string, bool) {
	if requests.IsJSONNull(raw) {
		return nil, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid "+field)
		return nil, false
	}
	return &s, true
}

func parseNullableUUIDField(c *gin.Context, raw json.RawMessage, field string) (*uuid.UUID, bool) {
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
