package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

type fakeTagService struct {
	listFn   func(ctx context.Context) ([]tagdomain.Tag, error)
	createFn func(ctx context.Context, name, slug string) (tagdomain.Tag, error)
	updateFn func(ctx context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error)
	mergeFn  func(ctx context.Context, sourceID, intoID uuid.UUID) error
	deleteFn func(ctx context.Context, id uuid.UUID) error
}

func (f *fakeTagService) List(ctx context.Context) ([]tagdomain.Tag, error) {
	return f.listFn(ctx)
}

func (f *fakeTagService) Create(ctx context.Context, name, slug string) (tagdomain.Tag, error) {
	return f.createFn(ctx, name, slug)
}

func (f *fakeTagService) Update(ctx context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error) {
	return f.updateFn(ctx, id, name, slug)
}

func (f *fakeTagService) Merge(ctx context.Context, sourceID, intoID uuid.UUID) error {
	return f.mergeFn(ctx, sourceID, intoID)
}

func (f *fakeTagService) Delete(ctx context.Context, id uuid.UUID) error {
	return f.deleteFn(ctx, id)
}

func TestListTagsHandlerReturnsItems(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := &fakeTagService{
		listFn: func(context.Context) ([]tagdomain.Tag, error) {
			return []tagdomain.Tag{
				{UUID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "Go", Slug: "go", CreatedAt: now},
				{UUID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Name: "Rust", Slug: "rust", CreatedAt: now},
			}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodGet, "/api/v1/tags", nil)

	listTagsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	items, ok := envelope.Data.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
}

func TestAdminCreateTagHandlerCreatesTag(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tagID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	svc := &fakeTagService{
		createFn: func(_ context.Context, name, slug string) (tagdomain.Tag, error) {
			require.Equal(t, "Go", name)
			require.Equal(t, "go", slug)
			return tagdomain.Tag{UUID: tagID, Name: name, Slug: slug, CreatedAt: now}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/tags", bytes.NewBufferString(`{"name":"Go","slug":"go"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminCreateTagHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, tagID.String(), data["id"])
	require.Equal(t, "go", data["slug"])
}

func TestAdminDeleteTagHandlerMapsInUse(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tagID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	svc := &fakeTagService{
		deleteFn: func(context.Context, uuid.UUID) error {
			return tagdomain.ErrInUse
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: tagID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodDelete, "/api/v1/admin/tags/"+tagID.String(), nil)

	adminDeleteTagHandler(svc)(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "tag_in_use", envelope.Error.Code)
}

func TestAdminMergeTagHandlerReturnsTargetTag(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	sourceID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	intoID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := &fakeTagService{
		mergeFn: func(context.Context, uuid.UUID, uuid.UUID) error { return nil },
		listFn: func(context.Context) ([]tagdomain.Tag, error) {
			return []tagdomain.Tag{{UUID: intoID, Name: "Target", Slug: "target", CreatedAt: now}}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: sourceID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/tags/"+sourceID.String()+"/merge", bytes.NewBufferString(`{"into_id":"`+intoID.String()+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	adminMergeTagHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, intoID.String(), data["id"])
}

func TestMapTagErrorValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	require.True(t, mapTagError(c, tagservice.ErrValidation))
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}
