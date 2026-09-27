package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	getFn    func(ctx context.Context, slug string) (categorydomain.Category, error)
	createFn func(ctx context.Context, in categoryservice.CreateInput) (categorydomain.Category, error)
	updateFn func(ctx context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error)
	deleteFn func(ctx context.Context, id uuid.UUID) error
	moveFn   func(ctx context.Context, id uuid.UUID, parentID, beforeID *uuid.UUID) (categorydomain.Category, error)
}

func (f *fakeCategoryService) List(ctx context.Context) ([]categorydomain.Category, error) {
	return f.listFn(ctx)
}

func (f *fakeCategoryService) GetBySlug(ctx context.Context, slug string) (categorydomain.Category, error) {
	if f.getFn != nil {
		return f.getFn(ctx, slug)
	}

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

func sampleCategory() categorydomain.Category {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	return categorydomain.Category{UUID: uuid.New(), Name: "Tech", Slug: "tech", Lft: 1, Rgt: 2, CreatedAt: now, UpdatedAt: now}
}

func TestListCategoriesHandlerFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeCategoryService{listFn: func(context.Context) ([]categorydomain.Category, error) {
		return nil, errors.New("db down")
	}}

	for name, handler := range map[string]gin.HandlerFunc{
		"public": listCategoriesHandler(svc),
		"admin":  adminListCategoriesHandler(svc),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/"})
			require.Equal(t, nethttp.StatusInternalServerError, w.Code)
			require.Equal(t, "internal_error", errorCode(body))
			require.Equal(t, "Failed to list categories", errorMessage(body))
		})
	}
}

func TestAdminListCategoriesHandler(t *testing.T) {
	t.Parallel()

	svc := &fakeCategoryService{listFn: func(context.Context) ([]categorydomain.Category, error) {
		return []categorydomain.Category{sampleCategory()}, nil
	}}

	w, body := runProfile(t, adminListCategoriesHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/api/v1/admin/categories"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Len(t, dataOf(body)["items"], 1)
}

func TestGetCategoryHandler(t *testing.T) {
	t.Parallel()

	cat := sampleCategory()
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "found", status: nethttp.StatusOK},
		{name: "not found", err: categorydomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found", message: "Category not found"},
		{name: "failure", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to load category"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotSlug string

			svc := &fakeCategoryService{getFn: func(_ context.Context, slug string) (categorydomain.Category, error) {
				gotSlug = slug
				return cat, tc.err
			}}
			w, body := runProfile(t, getCategoryHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/api/v1/categories/tech", param: "tech"})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
			require.Equal(t, "tech", gotSlug)

			if tc.status == nethttp.StatusOK {
				require.Equal(t, cat.UUID.String(), dataOf(body)["id"])
			}
		})
	}
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

func TestAdminCreateCategoryHandlerInputs(t *testing.T) {
	t.Parallel()

	parentID, imageID, beforeID := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
		check  func(t *testing.T, in categoryservice.CreateInput)
	}{
		{
			name:   "passes tree and image ids",
			body:   `{"name":"Go","parentId":"` + parentID.String() + `","imageId":"` + imageID.String() + `","beforeId":"` + beforeID.String() + `"}`,
			status: nethttp.StatusCreated,
			check: func(t *testing.T, in categoryservice.CreateInput) {
				t.Helper()
				require.Equal(t, &parentID, in.ParentID)
				require.Equal(t, &imageID, in.ImageID)
				require.Equal(t, &beforeID, in.BeforeID)
			},
		},
		{
			name: "absent ids stay nil", body: `{"name":"Go"}`, status: nethttp.StatusCreated,
			check: func(t *testing.T, in categoryservice.CreateInput) {
				t.Helper()
				require.Nil(t, in.ParentID)
				require.Nil(t, in.ImageID)
				require.Nil(t, in.BeforeID)
			},
		},
		{name: "missing name", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "invalid parent id", body: `{"name":"Go","parentId":"nope"}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "slug conflict", body: `{"name":"Go"}`, err: categorydomain.ErrConflict, status: nethttp.StatusConflict, code: "conflict"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got categoryservice.CreateInput

			svc := &fakeCategoryService{createFn: func(_ context.Context, in categoryservice.CreateInput) (categorydomain.Category, error) {
				got = in
				return sampleCategory(), tc.err
			}}
			w, body := runProfile(t, adminCreateCategoryHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/admin/categories", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestAdminUpdateCategoryHandler(t *testing.T) {
	t.Parallel()

	catID, imageID := uuid.New(), uuid.New()
	tests := []struct {
		name    string
		param   string
		body    string
		err     error
		status  int
		code    string
		message string
		check   func(t *testing.T, in categoryservice.UpdateInput)
	}{
		{name: "invalid id", param: "nope", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid category id"},
		{name: "malformed body", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error", message: "The given data was invalid."},
		{name: "parent cannot change", body: `{"parentId":null}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "parentId cannot be updated via PATCH; use move"},
		{name: "invalid name", body: `{"name":1}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid name"},
		{name: "invalid slug", body: `{"slug":false}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid slug"},
		{name: "invalid description", body: `{"description":1}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid description"},
		{name: "invalid image id", body: `{"imageId":"nope"}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid imageId"},
		{
			name: "updates every field", status: nethttp.StatusOK,
			body: `{"name":"Go","slug":"go","description":"Gophers","imageId":"` + imageID.String() + `"}`,
			check: func(t *testing.T, in categoryservice.UpdateInput) {
				t.Helper()
				require.Equal(t, "Go", *in.Name)
				require.Equal(t, "go", *in.Slug)
				require.Equal(t, "Gophers", *in.Description)
				require.True(t, in.ImageIDProvided)
				require.Equal(t, &imageID, in.ImageID)
			},
		},
		{
			name: "null description and image clear them", status: nethttp.StatusOK,
			body: `{"description":null,"imageId":null}`,
			check: func(t *testing.T, in categoryservice.UpdateInput) {
				t.Helper()
				require.Nil(t, in.Name)
				require.Empty(t, *in.Description)
				require.True(t, in.ImageIDProvided)
				require.Nil(t, in.ImageID)
			},
		},
		{name: "not found", body: `{"name":"Go"}`, err: categorydomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found", message: "Category not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotID uuid.UUID
				got   categoryservice.UpdateInput
			)

			svc := &fakeCategoryService{updateFn: func(_ context.Context, id uuid.UUID, in categoryservice.UpdateInput) (categorydomain.Category, error) {
				gotID, got = id, in
				return sampleCategory(), tc.err
			}}

			param := tc.param
			if param == "" {
				param = catID.String()
			}

			w, body := runProfile(t, adminUpdateCategoryHandler(svc), profileRequest{
				method: nethttp.MethodPatch, target: "/api/v1/admin/categories/" + param, contentType: jsonContent, body: tc.body, param: param,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))

			if tc.check != nil {
				require.Equal(t, catID, gotID)
				tc.check(t, got)
			}
		})
	}
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

func TestAdminDeleteCategoryHandlerRejectsInvalidID(t *testing.T) {
	t.Parallel()

	svc := &fakeCategoryService{deleteFn: func(context.Context, uuid.UUID) error {
		t.Fatal("delete should not be called")
		return nil
	}}
	w, body := runProfile(t, adminDeleteCategoryHandler(svc), profileRequest{method: nethttp.MethodDelete, target: "/", param: "nope"})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "validation_error", errorCode(body))
	require.Equal(t, "Invalid category id", errorMessage(body))
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
		bytes.NewBufferString(`{"parentId":"`+childID.String()+`"}`))
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
		bytes.NewBufferString(`{"beforeId":null}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminMoveCategoryHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestAdminMoveCategoryHandler(t *testing.T) {
	t.Parallel()

	catID, parentID, beforeID := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name       string
		param      string
		body       string
		status     int
		message    string
		wantParent *uuid.UUID
		wantBefore *uuid.UUID
	}{
		{name: "invalid id", param: "nope", body: `{}`, status: nethttp.StatusBadRequest, message: "Invalid category id"},
		{name: "malformed body", body: `[`, status: nethttp.StatusBadRequest, message: "The given data was invalid."},
		{name: "parent not a string", body: `{"parentId":5}`, status: nethttp.StatusBadRequest, message: "Invalid parentId"},
		{name: "parent not a uuid", body: `{"parentId":"nope"}`, status: nethttp.StatusBadRequest, message: "Invalid parentId"},
		{name: "before not a uuid", body: `{"parentId":null,"beforeId":"nope"}`, status: nethttp.StatusBadRequest, message: "Invalid beforeId"},
		{name: "moves to root", body: `{"parentId":null}`, status: nethttp.StatusOK},
		{
			name: "moves under parent before sibling", status: nethttp.StatusOK, wantParent: &parentID, wantBefore: &beforeID,
			body: `{"parentId":"` + parentID.String() + `","beforeId":" ` + beforeID.String() + ` "}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				called            bool
				gotParent, gotBef *uuid.UUID
			)

			svc := &fakeCategoryService{moveFn: func(_ context.Context, id uuid.UUID, parent, before *uuid.UUID) (categorydomain.Category, error) {
				require.Equal(t, catID, id)

				called, gotParent, gotBef = true, parent, before

				return sampleCategory(), nil
			}}

			param := tc.param
			if param == "" {
				param = catID.String()
			}

			w, body := runProfile(t, adminMoveCategoryHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/move", contentType: jsonContent, body: tc.body, param: param,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.message, errorMessage(body))
			require.Equal(t, tc.status == nethttp.StatusOK, called)

			if called {
				require.Equal(t, tc.wantParent, gotParent)
				require.Equal(t, tc.wantBefore, gotBef)
			}
		})
	}
}

func TestMapCategoryErrorValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	require.True(t, mapCategoryError(c, categoryservice.ErrValidation))
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestParseOptionalUUIDString(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	str := func(s string) *string { return &s }

	tests := []struct {
		name    string
		raw     *string
		want    *uuid.UUID
		wantErr string
	}{
		{name: "nil", raw: nil},
		{name: "blank", raw: str("  ")},
		{name: "uuid with padding", raw: str(" " + id.String() + " "), want: &id},
		{name: "invalid", raw: str("nope"), wantErr: `invalid "parentId"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOptionalUUIDString(tc.raw, "parentId")
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
