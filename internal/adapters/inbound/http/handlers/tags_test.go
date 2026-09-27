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
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/tags", nil)

	listTagsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	items, ok := envelope.Data.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
}

func TestListTagsHandlerFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeTagService{listFn: func(context.Context) ([]tagdomain.Tag, error) { return nil, errors.New("db down") }}
	w, body := runProfile(t, listTagsHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/api/v1/tags"})
	require.Equal(t, nethttp.StatusInternalServerError, w.Code)
	require.Equal(t, "internal_error", errorCode(body))
	require.Equal(t, "Failed to list tags", errorMessage(body))
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
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/tags", bytes.NewBufferString(`{"name":"Go","slug":"go"}`))
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

func TestAdminCreateTagHandlerFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "missing name", body: `{"slug":"go"}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "slug taken", body: `{"name":"Go"}`, err: tagdomain.ErrConflict, status: nethttp.StatusConflict, code: "conflict"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeTagService{createFn: func(context.Context, string, string) (tagdomain.Tag, error) { return tagdomain.Tag{}, tc.err }}
			w, body := runProfile(t, adminCreateTagHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/admin/tags", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestAdminUpdateTagHandler(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	tests := []struct {
		name   string
		param  string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "invalid id", param: "nope", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "malformed body", param: tagID.String(), body: `{`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "not found", param: tagID.String(), body: `{"name":"Go"}`, err: tagdomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found"},
		{name: "updates", param: " " + tagID.String() + " ", body: `{"name":"Go","slug":"golang"}`, status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotID uuid.UUID
			var gotName, gotSlug *string
			svc := &fakeTagService{updateFn: func(_ context.Context, id uuid.UUID, name, slug *string) (tagdomain.Tag, error) {
				gotID, gotName, gotSlug = id, name, slug
				return tagdomain.Tag{UUID: id, Name: "Go", Slug: "golang"}, tc.err
			}}
			w, body := runProfile(t, adminUpdateTagHandler(svc), profileRequest{
				method: nethttp.MethodPatch, target: "/", param: tc.param, contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, tagID, gotID)
				require.Equal(t, "Go", *gotName)
				require.Equal(t, "golang", *gotSlug)
				require.Equal(t, "golang", dataOf(body)["slug"])
			}
		})
	}
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
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodDelete, "/api/v1/admin/tags/"+tagID.String(), nil)

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
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/tags/"+sourceID.String()+"/merge", bytes.NewBufferString(`{"intoId":"`+intoID.String()+`"}`))
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

func TestAdminMergeTagHandlerFailures(t *testing.T) {
	t.Parallel()

	sourceID, intoID := uuid.New(), uuid.New()
	into := `{"intoId":"` + intoID.String() + `"}`
	tests := []struct {
		name     string
		param    string
		body     string
		mergeErr error
		listErr  error
		list     []tagdomain.Tag
		status   int
		code     string
		message  string
	}{
		{name: "invalid source id", param: "nope", body: into, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid tag id"},
		{name: "invalid into id", param: sourceID.String(), body: `{"intoId":"nope"}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "The given data was invalid."},
		{name: "merge fails", param: sourceID.String(), body: into, mergeErr: tagdomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found", message: "Tag not found"},
		{name: "reload fails", param: sourceID.String(), body: into, listErr: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to load merged tag"},
		{
			name: "target missing after merge", param: sourceID.String(), body: into, list: []tagdomain.Tag{{UUID: sourceID}},
			status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to load merged tag",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeTagService{
				mergeFn: func(context.Context, uuid.UUID, uuid.UUID) error { return tc.mergeErr },
				listFn:  func(context.Context) ([]tagdomain.Tag, error) { return tc.list, tc.listErr },
			}
			w, body := runProfile(t, adminMergeTagHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/", param: tc.param, contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.message, errorMessage(body))
		})
	}
}

func TestAdminDeleteTagHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		param  string
		status int
		code   string
	}{
		{name: "invalid id", param: "nope", status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "deletes", param: uuid.NewString(), status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeTagService{deleteFn: func(context.Context, uuid.UUID) error { return nil }}
			w, body := runProfile(t, adminDeleteTagHandler(svc), profileRequest{method: nethttp.MethodDelete, target: "/", param: tc.param})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestMapTagErrorValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	require.True(t, mapTagError(c, tagservice.ErrValidation))
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}
