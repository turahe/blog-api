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
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
	tagservice "github.com/turahe/blog-api/internal/core/tag/service"
)

type fakePostAdminService struct {
	listAdminFn func(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error)
	createFn    func(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error)
	updateFn    func(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error)
}

func (f *fakePostAdminService) ListAdmin(ctx context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	return f.listAdminFn(ctx, filter)
}

func (f *fakePostAdminService) CreateDraft(ctx context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error) {
	return f.createFn(ctx, authorID, title, slug, excerpt, content, categoryID, tags)
}

func (f *fakePostAdminService) Update(ctx context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
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
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil, nil
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
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil, nil
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
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(_ context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			require.Equal(t, postID, id)
			require.Equal(t, userID, actorID)
			require.True(t, unrestricted)
			require.NotNil(t, in.Slug)
			require.Equal(t, "taken-slug", *in.Slug)
			return postdomain.Post{}, nil, postservice.ErrConflict
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

func TestAdminCreatePostHandlerReturnsTags(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	postID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	tagID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	now := time.Date(2026, 7, 31, 13, 0, 0, 0, time.UTC)

	svc := &fakePostAdminService{
		createFn: func(_ context.Context, authorID uuid.UUID, title, slug, excerpt, content string, categoryID *uuid.UUID, tags *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			require.Equal(t, userID, authorID)
			require.Equal(t, "Title", title)
			require.Equal(t, "title", slug)
			require.Equal(t, "Excerpt", excerpt)
			require.Equal(t, "Content", content)
			require.Nil(t, categoryID)
			require.NotNil(t, tags)
			require.Equal(t, []string{"Go"}, *tags)
			return postdomain.Post{
				ID:        postID,
				AuthorID:  authorID,
				Title:     title,
				Slug:      slug,
				Excerpt:   excerpt,
				Content:   content,
				Status:    postdomain.StatusDraft,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
			}, []tagdomain.Tag{{ID: tagID, Name: "Go", Slug: "go", CreatedAt: now}}, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/posts", bytes.NewBufferString(`{"title":"Title","slug":"title","excerpt":"Excerpt","content":"Content","tags":["Go"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminCreatePostHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "go", data["tags"].([]any)[0].(map[string]any)["slug"])
}

func TestAdminUpdatePostHandlerAllowsTagsOnlyPatch(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	postID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	userID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tagID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)

	svc := &fakePostAdminService{
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(_ context.Context, id, actorID uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			require.Equal(t, postID, id)
			require.Equal(t, userID, actorID)
			require.True(t, unrestricted)
			require.Nil(t, in.Title)
			require.Nil(t, in.Slug)
			require.Nil(t, in.Excerpt)
			require.Nil(t, in.Content)
			require.NotNil(t, in.Tags)
			require.Equal(t, []string{"Go"}, *in.Tags)
			return postdomain.Post{
				ID:        postID,
				AuthorID:  userID,
				Title:     "Title",
				Slug:      "title",
				Status:    postdomain.StatusDraft,
				Version:   2,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now,
			}, []tagdomain.Tag{{ID: tagID, Name: "Go", Slug: "go", CreatedAt: now}}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: postID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPatch, "/api/v1/admin/posts/"+postID.String(), bytes.NewBufferString(`{"tags":["Go"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminUpdatePostHandlerWithDeps(svc, fakeRoleLookup{names: []string{"admin"}})(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "go", data["tags"].([]any)[0].(map[string]any)["slug"])
}

func TestAdminCreatePostHandlerMapsTagValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	svc := &fakePostAdminService{
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			return postdomain.Post{}, nil, tagservice.ErrValidation
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected update call")
			return postdomain.Post{}, nil, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/posts", bytes.NewBufferString(`{"title":"Title","slug":"title","tags":["bad tag"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminCreatePostHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestAdminUpdatePostHandlerMapsTagConflict(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	postID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	userID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	svc := &fakePostAdminService{
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			return postdomain.Post{}, nil, tagdomain.ErrConflict
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: postID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPatch, "/api/v1/admin/posts/"+postID.String(), bytes.NewBufferString(`{"tags":["Go"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminUpdatePostHandlerWithDeps(svc, fakeRoleLookup{names: []string{"admin"}})(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "conflict", envelope.Error.Code)
}

func TestAdminUpdatePostHandlerMapsTagNotFound(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	postID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	userID := uuid.MustParse("12121212-1212-1212-1212-121212121212")
	svc := &fakePostAdminService{
		createFn: func(context.Context, uuid.UUID, string, string, string, string, *uuid.UUID, *[]string) (postdomain.Post, []tagdomain.Tag, error) {
			t.Fatal("unexpected create call")
			return postdomain.Post{}, nil, nil
		},
		updateFn: func(context.Context, uuid.UUID, uuid.UUID, bool, postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
			return postdomain.Post{}, nil, tagdomain.ErrNotFound
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: postID.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPatch, "/api/v1/admin/posts/"+postID.String(), bytes.NewBufferString(`{"tags":["Go"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(contextUserIDKey, userID)

	adminUpdatePostHandlerWithDeps(svc, fakeRoleLookup{names: []string{"editor"}})(c)

	require.Equal(t, nethttp.StatusNotFound, w.Code)
	var envelope Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "not_found", envelope.Error.Code)
}
