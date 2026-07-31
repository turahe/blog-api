package http

import (
	"bytes"
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryservice "github.com/turahe/blog-api/internal/core/category/service"
)

type fakeCategoryService struct {
	listFn    func(ctx context.Context) ([]categorydomain.Category, error)
	getFn     func(ctx context.Context, slug string) (categorydomain.Category, error)
	createFn  func(ctx context.Context, in categorydomain.CreateInput) (categorydomain.Category, error)
	updateFn  func(ctx context.Context, id uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error)
	deleteFn  func(ctx context.Context, id uuid.UUID) error
	reorderFn func(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error
}

func (f *fakeCategoryService) List(ctx context.Context) ([]categorydomain.Category, error) {
	return f.listFn(ctx)
}

func (f *fakeCategoryService) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	return f.getFn(ctx, slug)
}

func (f *fakeCategoryService) Create(ctx context.Context, in categorydomain.CreateInput) (categorydomain.Category, error) {
	return f.createFn(ctx, in)
}

func (f *fakeCategoryService) Update(ctx context.Context, id uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error) {
	return f.updateFn(ctx, id, in)
}

func (f *fakeCategoryService) Delete(ctx context.Context, id uuid.UUID) error {
	return f.deleteFn(ctx, id)
}

func (f *fakeCategoryService) Reorder(ctx context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error {
	return f.reorderFn(ctx, parentID, orderedIDs)
}

func TestAdminCreateCategoryHandlerCreates(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	svc := &fakeCategoryService{
		createFn: func(_ context.Context, in categorydomain.CreateInput) (categorydomain.Category, error) {
			require.Equal(t, "News", in.Name)
			return categorydomain.Category{
				ID: id, Name: in.Name, Slug: "news", SortOrder: 0, CreatedAt: now, UpdatedAt: now,
			}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/categories", bytes.NewBufferString(`{"name":"News"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminCreateCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
}

func TestAdminDeleteCategoryHandlerMapsHasChildren(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	svc := &fakeCategoryService{
		deleteFn: func(_ context.Context, got uuid.UUID) error {
			require.Equal(t, id, got)
			return categorydomain.ErrHasChildren
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: id.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodDelete, "/api/v1/admin/categories/"+id.String(), nil)

	adminDeleteCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "category_has_children", envelope.Error.Code)
}

func TestAdminReorderCategoriesHandler(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	a := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	b := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	svc := &fakeCategoryService{
		reorderFn: func(_ context.Context, parentID *uuid.UUID, ordered []uuid.UUID) error {
			require.Nil(t, parentID)
			require.Equal(t, []uuid.UUID{b, a}, ordered)
			return nil
		},
	}

	body := `{"ordered_ids":["bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"]}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/categories/reorder", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	adminReorderCategoriesHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
}

func TestAdminUpdateCategoryHandlerClearsParent(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	now := time.Now().UTC()
	svc := &fakeCategoryService{
		updateFn: func(_ context.Context, got uuid.UUID, in categorydomain.UpdateInput) (categorydomain.Category, error) {
			require.Equal(t, id, got)
			require.True(t, in.ParentID.Present)
			require.Nil(t, in.ParentID.Value)
			return categorydomain.Category{ID: id, Name: "X", Slug: "x", CreatedAt: now, UpdatedAt: now}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: id.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPatch, "/api/v1/admin/categories/"+id.String(), bytes.NewBufferString(`{"parent_id":null}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminUpdateCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
}

func TestMapCategoryValidationError(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, mapCategoryError(c, categoryservice.ErrValidation))
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}
