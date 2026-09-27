package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postports "github.com/turahe/blog-api/internal/core/post/ports"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

// fakePostRepo backs a real PostService; methods a test does not reach stay unimplemented.
type fakePostRepo struct {
	postports.Repository

	post      postdomain.Post
	published postdomain.ListResult
	err       error

	slug       string
	listFilter postdomain.ListFilter
	cover      *uuid.UUID
}

func (f *fakePostRepo) GetByID(context.Context, uuid.UUID) (postdomain.Post, error) {
	return f.post, f.err
}

func (f *fakePostRepo) SetCoverImage(_ context.Context, _ uuid.UUID, mediaID *uuid.UUID, _ time.Time) error {
	f.cover = mediaID
	return nil
}

func (f *fakePostRepo) GetPublishedBySlug(_ context.Context, slug string) (postdomain.Post, error) {
	f.slug = slug
	return f.post, f.err
}

func (f *fakePostRepo) ListPublished(_ context.Context, filter postdomain.ListFilter) (postdomain.ListResult, error) {
	f.listFilter = filter
	return f.published, f.err
}

type fakePostMediaRepo struct {
	replaced []mediadomain.PostMediaItem
}

func (f *fakePostMediaRepo) ReplaceAll(_ context.Context, _ uuid.UUID, items []mediadomain.PostMediaItem) error {
	f.replaced = items
	return nil
}

func (f *fakePostMediaRepo) ListByPostID(context.Context, uuid.UUID) ([]mediadomain.PostMediaItem, error) {
	return f.replaced, nil
}

// fakeMediaRepo serves assets by id; unknown ids are mediadomain.ErrNotFound.
type fakeMediaRepo struct {
	mediaports.Repository

	assets map[uuid.UUID]mediadomain.MediaAsset
}

func (f fakeMediaRepo) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, ok := f.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, mediadomain.ErrNotFound
	}

	return asset, nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func TestAdminCreatePostHandlerRequests(t *testing.T) {
	t.Parallel()

	categoryID := uuid.New()
	tests := []struct {
		name         string
		user         *uuid.UUID
		body         string
		status       int
		code         string
		wantCategory *uuid.UUID
	}{
		{name: "anonymous", body: `{}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{
			name: "passes the category", user: &testUserID, status: nethttp.StatusCreated, wantCategory: &categoryID,
			body: `{"title":"T","slug":"t","content":"C","categoryId":"` + categoryID.String() + `"}`,
		},
		{name: "invalid category", user: &testUserID, body: `{"title":"T","slug":"t","content":"C","categoryId":"nope"}`, status: nethttp.StatusBadRequest, code: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotCategory *uuid.UUID

			svc := &fakePostAdminService{createFn: func(_ context.Context, _ uuid.UUID, _, _, _, _ string, categoryID *uuid.UUID, _ *[]string) (postdomain.Post, []tagdomain.Tag, error) {
				gotCategory = categoryID
				return postdomain.Post{UUID: uuid.New()}, nil, nil
			}}
			w, body := runProfile(t, adminCreatePostHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/admin/posts", contentType: jsonContent, body: tc.body, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.wantCategory, gotCategory)
		})
	}
}

func TestAdminListPostsHandlerRequests(t *testing.T) {
	t.Parallel()

	authorID := uuid.New()
	tests := []struct {
		name    string
		user    *uuid.UUID
		query   string
		roles   RoleLookup
		listErr error
		status  int
		code    string
		message string
		check   func(t *testing.T, filter postdomain.AdminListFilter)
	}{
		{name: "anonymous", status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "invalid author", user: &testUserID, query: "?authorId=nope", status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid authorId"},
		{name: "invalid category", user: &testUserID, query: "?categoryId=nope", status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid categoryId"},
		{
			name: "role lookup failure", user: &testUserID, roles: fakeRoleLookup{err: errors.New("db down")},
			status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to resolve roles",
		},
		{name: "invalid trashed", user: &testUserID, query: "?trashed=maybe", status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid trashed"},
		{
			name: "service validation", user: &testUserID, listErr: postservice.ErrValidation,
			status: nethttp.StatusBadRequest, code: "validation_error", message: postservice.ErrValidation.Error(),
		},
		{
			name: "no role lookup scopes to own posts", user: &testUserID, query: "?trashed=true&authorId=" + authorID.String(), status: nethttp.StatusOK,
			check: func(t *testing.T, filter postdomain.AdminListFilter) {
				t.Helper()
				require.True(t, filter.Trashed)
				require.Equal(t, &authorID, filter.AuthorUUID)
				require.Equal(t, &testUserID, filter.ScopeAuthorUUID)
			},
		},
		{
			name: "editor sees every post", user: &testUserID, roles: fakeRoleLookup{names: []string{" Editor "}}, status: nethttp.StatusOK,
			check: func(t *testing.T, filter postdomain.AdminListFilter) {
				t.Helper()
				require.Nil(t, filter.ScopeAuthorUUID)
				require.False(t, filter.Trashed)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got postdomain.AdminListFilter

			svc := &fakePostAdminService{listAdminFn: func(_ context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
				got = filter
				return postdomain.ListResult{Page: 1, PerPage: 20}, tc.listErr
			}}
			w, body := runProfile(t, adminListPostsHandlerWithDeps(svc, tc.roles), profileRequest{
				method: nethttp.MethodGet, target: "/api/v1/admin/posts" + tc.query, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.message != "" {
				require.Equal(t, tc.message, errorMessage(body))
			}

			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestAdminUpdatePostHandlerRequests(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	tests := []struct {
		name    string
		user    *uuid.UUID
		param   string
		body    string
		roles   RoleLookup
		status  int
		code    string
		message string
		check   func(t *testing.T, unrestricted bool, in postdomain.UpdateInput)
	}{
		{name: "anonymous", param: postID.String(), body: `{}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "invalid id", user: &testUserID, param: "nope", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid post id"},
		{name: "malformed body", user: &testUserID, param: postID.String(), body: `{`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{
			name: "role lookup failure", user: &testUserID, param: postID.String(), body: `{"title":"T"}`, roles: fakeRoleLookup{err: errors.New("db down")},
			status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to resolve roles",
		},
		{name: "category not a string", user: &testUserID, param: postID.String(), body: `{"categoryId":7}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid categoryId"},
		{name: "category not a uuid", user: &testUserID, param: postID.String(), body: `{"categoryId":"nope"}`, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid categoryId"},
		{
			name: "comment policy without role lookup", user: &testUserID, param: postID.String(), body: `{"commentPolicy":"read_only"}`, status: nethttp.StatusOK,
			check: func(t *testing.T, unrestricted bool, in postdomain.UpdateInput) {
				t.Helper()
				require.False(t, unrestricted)
				require.Equal(t, postdomain.CommentPolicy("read_only"), *in.CommentPolicy)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotUnrestricted bool
				got             postdomain.UpdateInput
			)

			svc := &fakePostAdminService{updateFn: func(_ context.Context, _, _ uuid.UUID, unrestricted bool, in postdomain.UpdateInput) (postdomain.Post, []tagdomain.Tag, error) {
				gotUnrestricted, got = unrestricted, in
				return postdomain.Post{UUID: postID}, nil, nil
			}}
			w, body := runProfile(t, adminUpdatePostHandlerWithDeps(svc, tc.roles), profileRequest{
				method: nethttp.MethodPatch, target: "/", contentType: jsonContent, body: tc.body, param: tc.param, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.message != "" {
				require.Equal(t, tc.message, errorMessage(body))
			}

			if tc.check != nil {
				tc.check(t, gotUnrestricted, got)
			}
		})
	}
}

func TestAdminReplacePostMediaHandler(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	cover := mediadomain.MediaAsset{UUID: uuid.New(), Status: mediadomain.StatusReady, ContentType: "image/png"}
	attachment := mediadomain.MediaAsset{UUID: uuid.New(), Status: mediadomain.StatusReady, ContentType: "application/pdf"}
	items := `{"enforceCoverConsistency":false,"items":[` +
		`{"mediaAssetId":"` + cover.UUID.String() + `","kind":"cover"},` +
		`{"mediaAssetId":"` + attachment.UUID.String() + `","kind":"attachment","sortOrder":4}]}`

	tests := []struct {
		name        string
		param       string
		body        string
		noMedia     bool
		repoErr     error
		deleted     bool
		variantsErr error
		status      int
		code        string
		message     string
	}{
		{name: "invalid post id", param: "nope", body: items, status: nethttp.StatusBadRequest, code: "validation_error", message: "Invalid post id"},
		{name: "missing items", param: postID.String(), body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{
			name: "media not configured", param: postID.String(), body: items, noMedia: true,
			status: nethttp.StatusBadRequest, code: "validation_error", message: postservice.ErrValidation.Error() + ": media associations not configured",
		},
		{name: "post not found", param: postID.String(), body: items, repoErr: postdomain.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found", message: "Post not found"},
		{name: "trashed post", param: postID.String(), body: items, deleted: true, status: nethttp.StatusNotFound, code: "not_found", message: "Post not found"},
		{
			name: "repository failure", param: postID.String(), body: items, repoErr: errors.New("db down"),
			status: nethttp.StatusInternalServerError, code: "internal_error", message: "Failed to replace post media",
		},
		{name: "variants failure", param: postID.String(), body: items, variantsErr: mediaservice.ErrStorage, status: nethttp.StatusBadGateway, code: "storage_unavailable"},
		{name: "replaces media", param: postID.String(), body: items, status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			post := postdomain.Post{UUID: postID, Status: postdomain.StatusDraft}
			if tc.deleted {
				at := time.Now()
				post.DeletedAt = &at
			}

			repo := &fakePostRepo{post: post, err: tc.repoErr}
			postMedia := &fakePostMediaRepo{}
			posts := postservice.New(repo, nil, fixedClock{at: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})

			if !tc.noMedia {
				posts.WithMedia(postMedia, fakeMediaRepo{assets: map[uuid.UUID]mediadomain.MediaAsset{cover.UUID: cover, attachment.UUID: attachment}})
			}

			media := &fakeMediaService{variantsFn: func(_ context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error) {
				return map[uuid.UUID]map[string]string{assets[0].UUID: {"thumb": "https://img/cover"}}, tc.variantsErr
			}}

			w, body := runProfile(t, adminReplacePostMediaHandler(posts, media), profileRequest{
				method: nethttp.MethodPatch, target: "/media", contentType: jsonContent, body: tc.body, param: tc.param,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.message != "" {
				require.Equal(t, tc.message, errorMessage(body))
			}

			if tc.status != nethttp.StatusOK {
				return
			}

			require.Equal(t, &cover.UUID, repo.cover)
			require.Len(t, postMedia.replaced, 2)

			data := dataOf(body)
			require.Equal(t, postID.String(), data["postId"])

			rows, ok := data["items"].([]any)
			require.True(t, ok)
			require.Len(t, rows, 2)

			first := as[map[string]any](t, rows[0])
			require.Equal(t, "cover", first["kind"])
			require.Equal(t, map[string]any{"thumb": "https://img/cover"}, as[map[string]any](t, first["media"])["variants"])

			second := as[map[string]any](t, rows[1])
			require.Equal(t, attachment.UUID.String(), second["mediaAssetId"])
			require.InDelta(t, 4, second["sortOrder"], 0)
			require.Equal(t, attachment.UUID.String(), as[map[string]any](t, second["media"])["id"])
		})
	}
}

func TestPostMediaPayloadKeepsRowsWithoutAssets(t *testing.T) {
	t.Parallel()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	asset := sampleAsset()
	items := []mediadomain.PostMediaItem{
		{MediaAssetUUID: uuid.New(), Kind: mediadomain.KindAttachment},
		{MediaAssetUUID: asset.UUID, Kind: mediadomain.KindCover, Media: &asset},
	}

	payload, ok := postMediaPayload(c, nil, items)
	require.True(t, ok)
	require.Len(t, payload, 2)
	require.NotContains(t, payload[0], "media")
	require.Equal(t, asset.UUID.String(), as[gin.H](t, payload[1]["media"])["id"])
}
