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
			responses.Internal(c, err, "Failed to list categories")
			return
		}

		out := make([]gin.H, 0, len(items))
		for _, item := range items {
			out = append(out, responses.Category(item))
		}

		responses.Success(c, nethttp.StatusOK, gin.H{"items": out})
	}
}

// adminListCategoriesHandler godoc
//
//	@Summary	List categories (admin)
//	@Tags		admin
//	@Produce	json
//	@Security	Bearer
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Failure	403	{object}	responses.Envelope
//	@Router		/api/v1/admin/categories [get]
func adminListCategoriesHandler(cats categoryAPI) gin.HandlerFunc {
	return listCategoriesHandler(cats)
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
			responses.Failure(c, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Category not found")
			return
		}

		if err != nil {
			responses.Internal(c, err, "Failed to load category")
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

		parentID, err := parseOptionalUUIDString(req.ParentID, "parentId")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
			return
		}

		imageID, err := parseOptionalUUIDString(req.ImageID, "imageId")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
			return
		}

		beforeID, err := parseOptionalUUIDString(req.BeforeID, "beforeId")
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
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
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid category id")
			return
		}

		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			requests.FailValidation(c, err)
			return
		}

		if _, ok := raw["parentId"]; ok {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "parentId cannot be updated via PATCH; use move")
			return
		}

		in, ok := parseCategoryUpdate(c, raw)
		if !ok {
			return
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
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid category id")
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
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid category id")
			return
		}

		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			requests.FailValidation(c, err)
			return
		}

		parentRaw, ok := raw["parentId"]
		if !ok {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "parentId required")
			return
		}

		parentID, ok := parseNullableUUID(c, parentRaw, "parentId")
		if !ok {
			return
		}

		var beforeID *uuid.UUID
		if beforeRaw, hasBefore := raw["beforeId"]; hasBefore {
			beforeID, ok = parseNullableUUID(c, beforeRaw, "beforeId")
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

var categoryErrors = resourceErrors{
	service:    responses.ServiceCategories,
	name:       "Category",
	inUseCode:  "category_in_use",
	validation: categoryservice.ErrValidation,
	notFound:   categorydomain.ErrNotFound,
	conflict:   categorydomain.ErrConflict,
	inUse:      categorydomain.ErrInUse,
}

func mapCategoryError(c *gin.Context, err error) bool {
	return categoryErrors.write(c, err)
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
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid "+field)
		return nil, false
	}

	return &s, true
}

// parseCategoryUpdate maps a PATCH body onto UpdateInput, writing a 400 and
// returning false on malformed fields.
func parseCategoryUpdate(c *gin.Context, raw map[string]json.RawMessage) (categoryservice.UpdateInput, bool) {
	in := categoryservice.UpdateInput{}

	if v, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(v, &name); err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid name")
			return in, false
		}

		in.Name = &name
	}

	if v, ok := raw["slug"]; ok {
		var slug string
		if err := json.Unmarshal(v, &slug); err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid slug")
			return in, false
		}

		in.Slug = &slug
	}

	if v, ok := raw["description"]; ok {
		desc, ok := parseNullableString(c, v, "description")
		if !ok {
			return in, false
		}

		if desc == nil {
			empty := ""
			in.Description = &empty
		} else {
			in.Description = desc
		}
	}

	if v, ok := raw["imageId"]; ok {
		in.ImageIDProvided = true

		imageID, ok := parseNullableUUID(c, v, "imageId")
		if !ok {
			return in, false
		}

		in.ImageID = imageID
	}

	return in, true
}
