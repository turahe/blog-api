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
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type fakePostAdminService struct {
	listAdminFn func(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	updateFn    func(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error)
}

func (f *fakePostAdminService) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	return f.listAdminFn(ctx, filter)
}

func (f *fakePostAdminService) Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error) {
	return f.updateFn(ctx, id, actorID, unrestricted, in)
}

type fakeRoleLookup struct {
	names []string
	err   error
}

func (f fakeRoleLookup) ListRoleNames(context.Context, uuid.UUID) ([]string, error) {
	return f.names, f.err
}

func TestAdminListPostsHandlerReturnsMetaTotalAndScopesRestrictedAuthors(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	postID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	categoryID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	svc := &fakePostAdminService{
		listAdminFn: func(_ context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
			require.Equal(t, 2, filter.Page)
			require.Equal(t, 10, filter.PerPage)
			require.Equal(t, "draft", filter.Status)
			require.Equal(t, "hello", filter.Query)
			require.NotNil(t, filter.ScopeAuthorID)
			require.Equal(t, userID, *filter.ScopeAuthorID)
			return postdomain.ListResult{
				Items: []postdomain.Post{{
					ID:         postID,
					AuthorID:   userID,
					CategoryID: &categoryID,
					Title:      "Draft post",
					Slug:       "draft-post",
					Excerpt:    "Excerpt",
					Content:    "Content",
					Status:     postdomain.StatusDraft,
					Version:    1,
					CreatedAt:  now,
					UpdatedAt:  now,
				}},
				Total:   7,
				Page:    2,
				PerPage: 10,
			}, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodGet, "/api/v1/admin/posts?page=2&per_page=10&status=draft&q=hello", nil)
	c.Set(contextUserIDKey, userID)

	adminListPostsHandlerWithDeps(svc, fakeRoleLookup{names: []string{"author"}})(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	require.NotNil(t, envelope.Meta)
	require.Equal(t, int64(7), envelope.Meta.Total)
	data, ok := envelope.Data.([]any)
	require.True(t, ok)
	require.Len(t, data, 1)
}

func TestAdminUpdatePostHandlerRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	svc := &fakePostAdminService{
		listAdminFn: func(context.Context, postdomain.AdminListFilter) (postdomain.ListResult, error) {
			t.Fatal("unexpected list call")
			return postdomain.ListResult{}, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil
		},
	}

	postID := uuid.New()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: postID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPatch, "/api/v1/admin/posts/"+postID.String(), nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, uuid.New())

	adminUpdatePostHandlerWithDeps(svc, fakeRoleLookup{names: []string{"admin"}})(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestAdminUpdatePostHandlerMapsConflict(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	postID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	userID := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	svc := &fakePostAdminService{
		listAdminFn: func(context.Context, postdomain.AdminListFilter) (postdomain.ListResult, error) {
			t.Fatal("unexpected list call")
			return postdomain.ListResult{}, nil
		},
		updateFn: func(_ context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, error) {
			require.Equal(t, postID, id)
			require.Equal(t, userID, actorID)
			require.True(t, unrestricted)
			require.NotNil(t, in.Slug)
			require.Equal(t, "taken-slug", *in.Slug)
			return postdomain.Post{}, postservice.ErrConflict
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: postID.String()}}
	c.Request = httptest.NewRequest(
		nethttp.MethodPatch,
		"/api/v1/admin/posts/"+postID.String(),
		bytes.NewBufferString(`{"slug":"taken-slug"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminUpdatePostHandlerWithDeps(svc, fakeRoleLookup{names: []string{"editor"}})(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "conflict", envelope.Error.Code)
}
