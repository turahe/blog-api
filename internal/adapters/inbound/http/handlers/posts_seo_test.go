package handlers

import (
	"context"
	nethttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type fakeSEO struct {
	patch        postdomain.SEOPatch
	draft        postservice.SEODraft
	unrestricted bool
	slugAllowed  bool
	slug         string
	meta         postdomain.SEOMeta
	err          error
}

func (f *fakeSEO) GetSEO(_ context.Context, postID, _ uuid.UUID, unrestricted bool) (postservice.SEOView, error) {
	f.unrestricted = unrestricted

	return postservice.SEOView{PostUUID: postID, Slug: "a-post", SEO: postdomain.SEO{Title: "T"}}, f.err
}

func (f *fakeSEO) UpdateSEO(
	_ context.Context, postID, _ uuid.UUID, unrestricted, slugAllowed bool, patch postdomain.SEOPatch,
) (postservice.SEOView, error) {
	f.patch, f.unrestricted, f.slugAllowed = patch, unrestricted, slugAllowed

	return postservice.SEOView{PostUUID: postID, Slug: "a-post"}, f.err
}

func (f *fakeSEO) PreviewSEO(_ context.Context, _, _ uuid.UUID, _ bool, draft postservice.SEODraft) (postdomain.SEOPreview, error) {
	f.draft = draft
	return postdomain.SEOPreview{Search: postdomain.SearchPreview{Title: "Preview"}}, f.err
}

func (f *fakeSEO) SEOMeta(_ context.Context, slug string) (postdomain.SEOMeta, error) {
	f.slug = slug
	return f.meta, f.err
}

func seoTestAccess(unrestricted, slugAllowed bool) seoAccess {
	return seoAccess{
		unrestricted: func(*gin.Context, uuid.UUID) (bool, error) { return unrestricted, nil },
		slugAllowed:  func(*gin.Context) bool { return slugAllowed },
	}
}

func TestSEORoutesRequirePermissions(t *testing.T) {
	t.Parallel()

	enforcer := &recordingEnforcer{}
	c := NewControllers(Deps{Posts: postservice.New(nil, nil, nil), RBAC: enforcer}).Posts
	want := map[string]string{"get": permPostSEOView, "update": permPostSEOEdit, "preview": permPostSEOView}

	for name, handler := range map[string]gin.HandlerFunc{"get": c.SEOGet, "update": c.SEOUpdate, "preview": c.SEOPreview} {
		enforcer.checked = nil

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, name)
		require.Equal(t, "rbac.forbidden", errorCode(body), name)
		require.Equal(t, []string{want[name]}, enforcer.checked, name)
	}
}

func TestGetPostSEOListsEveryField(t *testing.T) {
	t.Parallel()

	svc := &fakeSEO{}
	postID := uuid.New()

	w, body := runProfile(t, adminGetPostSEOHandler(svc, seoTestAccess(true, false)), profileRequest{
		method: nethttp.MethodGet, target: "/", param: postID.String(), user: &testUserID,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	data := dataOf(body)
	assert.Equal(t, postID.String(), data["postId"])
	assert.Equal(t, "T", data["seoTitle"])
	assert.Empty(t, data["seoDescription"])
	assert.Nil(t, data["ogImageId"])
	assert.Equal(t, []any{}, data["seoKeywords"])
	assert.Equal(t, []any{}, data["warnings"])
	assert.True(t, svc.unrestricted)
}

func TestUpdatePostSEOBindsTriStateImages(t *testing.T) {
	t.Parallel()

	svc := &fakeSEO{}
	image := uuid.New()

	w, _ := runProfile(t, adminUpdatePostSEOHandler(svc, seoTestAccess(false, true)), profileRequest{
		method: nethttp.MethodPut, target: "/", param: uuid.NewString(), user: &testUserID,
		contentType: "application/json",
		body:        `{"seoTitle":"New","ogImageId":"` + image.String() + `","twitterImageId":null,"twitterCard":"summary"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	require.NotNil(t, svc.patch.Title)
	assert.Equal(t, "New", *svc.patch.Title)
	assert.Nil(t, svc.patch.Description, "omitted fields stay unchanged")
	assert.Equal(t, postdomain.OptionalUUID{Present: true, Value: &image}, svc.patch.OGImage)
	assert.Equal(t, postdomain.OptionalUUID{Present: true}, svc.patch.TwitterImage, "null clears the image")
	require.NotNil(t, svc.patch.TwitterCard)
	assert.Equal(t, postdomain.TwitterSummary, *svc.patch.TwitterCard)
	assert.True(t, svc.slugAllowed)
	assert.False(t, svc.unrestricted)
}

func TestUpdatePostSEOErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"violations": {
			err: &postdomain.SEOValidationError{Violations: []postdomain.FieldViolation{
				{Field: "seo_title", Code: postdomain.SEOCodeMarkup, Message: "no markup"},
			}},
			status: nethttp.StatusUnprocessableEntity, code: responses.ErrorCodeValidation,
		},
		"slug forbidden": {err: postdomain.ErrSlugEditForbidden, status: nethttp.StatusForbidden, code: responses.ErrorCodeForbidden},
		"not found":      {err: postdomain.ErrNotFound, status: nethttp.StatusNotFound, code: responses.ErrorCodeNotFound},
	}

	for name, tc := range cases {
		svc := &fakeSEO{err: tc.err}

		w, body := runProfile(t, adminUpdatePostSEOHandler(svc, seoTestAccess(false, false)), profileRequest{
			method: nethttp.MethodPut, target: "/", param: uuid.NewString(), user: &testUserID,
			contentType: "application/json", body: `{"slug":"x"}`,
		})
		require.Equal(t, tc.status, w.Code, name)
		assert.Equal(t, tc.code, errorCode(body), name)

		if name == "violations" {
			errBody, _ := body["error"].(map[string]any)
			details, _ := errBody["details"].([]any)
			require.Len(t, details, 1)
			first, _ := details[0].(map[string]any)
			assert.Equal(t, "seoTitle", first["field"])
			assert.Equal(t, postdomain.SEOCodeMarkup, first["code"])
		}
	}

	w, body := runProfile(t, adminUpdatePostSEOHandler(&fakeSEO{}, seoTestAccess(false, false)), profileRequest{
		method: nethttp.MethodPut, target: "/", param: uuid.NewString(), user: &testUserID,
		contentType: "application/json", body: `{"ogImageId":"not-a-uuid"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	assert.Equal(t, responses.ErrorCodeValidation, errorCode(body))
}

func TestPreviewPostSEOAcceptsDraftFields(t *testing.T) {
	t.Parallel()

	svc := &fakeSEO{}

	w, body := runProfile(t, adminPreviewPostSEOHandler(svc, seoTestAccess(false, false)), profileRequest{
		method: nethttp.MethodPost, target: "/", param: uuid.NewString(), user: &testUserID,
		contentType: "application/json", body: `{"title":"Draft","seoDescription":"D"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	require.NotNil(t, svc.draft.Title)
	assert.Equal(t, "Draft", *svc.draft.Title)
	require.NotNil(t, svc.draft.Patch.Description)
	assert.Equal(t, "D", *svc.draft.Patch.Description)

	search, _ := dataOf(body)["searchPreview"].(map[string]any)
	assert.Equal(t, "Preview", search["title"])

	w, _ = runProfile(t, adminPreviewPostSEOHandler(svc, seoTestAccess(false, false)), profileRequest{
		method: nethttp.MethodPost, target: "/", param: uuid.NewString(), user: &testUserID,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, "the body is optional")
}

func TestPublicSEOMetaSetsRobotsHeader(t *testing.T) {
	t.Parallel()

	svc := &fakeSEO{meta: postdomain.SEOMeta{Title: "T", Robots: "noindex,nofollow"}}

	w, body := runProfile(t, publicPostSEOMetaHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/", param: "a-post"})
	require.Equal(t, nethttp.StatusOK, w.Code)
	assert.Equal(t, "a-post", svc.slug)
	assert.Equal(t, "noindex,nofollow", w.Header().Get("X-Robots-Tag"))
	assert.Equal(t, "T", dataOf(body)["title"])

	svc.meta.Robots = "index,follow"
	w, _ = runProfile(t, publicPostSEOMetaHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/", param: "a-post"})
	assert.Empty(t, w.Header().Get("X-Robots-Tag"))

	svc.err = postdomain.ErrNotFound
	w, _ = runProfile(t, publicPostSEOMetaHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/", param: "gone"})
	assert.Equal(t, nethttp.StatusNotFound, w.Code)
}
