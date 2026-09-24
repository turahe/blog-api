package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
)

type fakeCategoryService struct {
	listFn   func(ctx context.Context) ([]categorydomain.Category, error)
	createFn func(ctx context.Context, in categoryservice.CreateInput) (categorydomain.Category, error)
	updateFn func(ctx context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error)
	deleteFn func(ctx context.Context, id uuid.UUID) error
	moveFn   func(ctx context.Context, id uuid.UUID, parentID, beforeID *uuid.UUID) (categorydomain.Category, error)
}

func (f *fakeCategoryService) List(ctx context.Context) ([]categorydomain.Category, error) {
	return f.listFn(ctx)
}

func (f *fakeCategoryService) GetBySlug(context.Context, string) (categorydomain.Category, error) {
	return categorydomain.Category{}, categorydomain.ErrNotFound
}

func (f *fakeCategoryService) Create(ctx context.Context, in categoryservice.CreateInput) (categorydomain.Category, error) {
	return f.createFn(ctx, in)
}

func (f *fakeCategoryService) Update(ctx context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error) {
	return f.updateFn(ctx, id, in)
}

func (f *fakeCategoryService) Delete(ctx context.Context, id uuid.UUID) error {
	return f.deleteFn(ctx, id)
}

func (f *fakeCategoryService) Move(ctx context.Context, id uuid.UUID, parentID, beforeID *uuid.UUID) (categorydomain.Category, error) {
	return f.moveFn(ctx, id, parentID, beforeID)
}

func TestListCategoriesHandlerReturnsItemsEnvelope(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	svc := &fakeCategoryService{
		listFn: func(context.Context) ([]categorydomain.Category, error) {
			return []categorydomain.Category{
				{
					UUID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "Tech", Slug: "tech",
					Lft: 1, Rgt: 2, Depth: 0, SortOrder: 0, CreatedAt: now, UpdatedAt: now,
				},
			}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/categories", nil)

	listCategoriesHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	items, ok := data["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.InDelta(t, 1, item["lft"], 0)
	require.InDelta(t, 2, item["rgt"], 0)
}

func TestAdminCreateCategoryHandlerCreatesCategory(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	catID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	svc := &fakeCategoryService{
		createFn: func(_ context.Context, in categoryservice.CreateInput) (categorydomain.Category, error) {
			require.Equal(t, "Technology", in.Name)
			require.Equal(t, "technology", in.Slug)

			return categorydomain.Category{
				UUID: catID, Name: in.Name, Slug: in.Slug,
				Lft: 1, Rgt: 2, Depth: 0, SortOrder: 0, CreatedAt: now, UpdatedAt: now,
			}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/categories",
		bytes.NewBufferString(`{"name":"Technology","slug":"technology"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminCreateCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, catID.String(), data["id"])
	require.Equal(t, "technology", data["slug"])
}

func TestAdminDeleteCategoryHandlerMapsInUse(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	catID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	svc := &fakeCategoryService{
		deleteFn: func(context.Context, uuid.UUID) error {
			return categorydomain.ErrInUse
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: catID.String()}}
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodDelete, "/api/v1/admin/categories/"+catID.String(), nil)

	adminDeleteCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "category_in_use", envelope.Error.Code)
}

func TestAdminDeleteCategoryHandlerReturns204(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	catID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	svc := &fakeCategoryService{
		deleteFn: func(context.Context, uuid.UUID) error { return nil },
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: catID.String()}}
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodDelete, "/api/v1/admin/categories/"+catID.String(), nil)

	adminDeleteCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
}

func TestAdminMoveCategoryHandlerRejectsCycle(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	catID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	childID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	svc := &fakeCategoryService{
		moveFn: func(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (categorydomain.Category, error) {
			return categorydomain.Category{}, categoryservice.ErrValidation
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: catID.String()}}
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/categories/"+catID.String()+"/move",
		bytes.NewBufferString(`{"parent_id":"`+childID.String()+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminMoveCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestAdminMoveCategoryHandlerRequiresParentID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	catID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	svc := &fakeCategoryService{
		moveFn: func(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (categorydomain.Category, error) {
			t.Fatal("move should not be called")
			return categorydomain.Category{}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: catID.String()}}
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/categories/"+catID.String()+"/move",
		bytes.NewBufferString(`{"before_id":null}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminMoveCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestMapCategoryErrorValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	require.True(t, mapCategoryError(c, categoryservice.ErrValidation))
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}
