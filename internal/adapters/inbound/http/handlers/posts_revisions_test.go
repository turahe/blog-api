package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type fakeRevisions struct {
	filter       postdomain.RevisionFilter
	ref          postdomain.RevisionRef
	unrestricted bool
	note         string
	editor       postdomain.Editor
	err          error
}

func (f *fakeRevisions) ListRevisions(_ context.Context, _ uuid.UUID, unrestricted bool, filter postdomain.RevisionFilter) (postdomain.RevisionPage, error) {
	f.filter, f.unrestricted = filter, unrestricted

	return postdomain.RevisionPage{Items: []postdomain.Revision{sampleRevision()}, Total: 1, Page: 1, PerPage: 20}, f.err
}

func (f *fakeRevisions) GetRevision(_ context.Context, _ uuid.UUID, ref postdomain.RevisionRef, _ uuid.UUID, unrestricted bool) (postdomain.Revision, error) {
	f.ref, f.unrestricted = ref, unrestricted
	return sampleRevision(), f.err
}

func (f *fakeRevisions) RestoreRevision(
	ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef, _ uuid.UUID, unrestricted bool, note string,
) (postservice.RestoreResult, error) {
	f.ref, f.unrestricted, f.note, f.editor = ref, unrestricted, note, postdomain.EditorFrom(ctx)

	return postservice.RestoreResult{
		Post:     postdomain.Post{UUID: postID, Title: "Restored", Status: postdomain.StatusDraft, CreatedAt: testTime, UpdatedAt: testTime},
		Revision: sampleRevision(),
		Skipped:  postservice.RestoreSkips{Categories: []uuid.UUID{}, Tags: []uuid.UUID{}, Media: []uuid.UUID{}},
	}, f.err
}

func sampleRevision() postdomain.Revision {
	return postdomain.Revision{
		UUID: uuid.New(), PostUUID: uuid.New(), Number: 2, Type: postdomain.RevisionUpdate,
		ChangedFields: []string{postdomain.FieldTitle},
		Diff:          map[string]any{postdomain.FieldTitle: map[string]any{"from": "a", "to": "b"}},
		Changelog:     "Updated title", AuthorUUID: &testUserID, CreatedAt: testTime,
		Snapshot: postdomain.Snapshot{Title: "b", Content: "full text", Status: postdomain.StatusDraft},
	}
}

func always(bool) allPosts { return func(*gin.Context) bool { return true } }

func TestRevisionRoutesRequirePermissions(t *testing.T) {
	t.Parallel()

	enforcer := &recordingEnforcer{}
	c := NewControllers(Deps{Posts: postservice.New(nil, nil, nil), RBAC: enforcer}).Posts
	want := map[string]string{
		"list": permPostRevisionsView, "get": permPostRevisionsView, "restore": permPostRevisionsRestore,
	}

	for name, handler := range map[string]gin.HandlerFunc{"list": c.RevisionsList, "get": c.RevisionGet, "restore": c.RevisionRestore} {
		enforcer.checked = nil

		w, body := runProfile(t, handler, profileRequest{method: nethttp.MethodGet, target: "/", user: &testUserID})
		require.Equal(t, nethttp.StatusForbidden, w.Code, name)
		require.Equal(t, "rbac.forbidden", errorCode(body), name)
		require.Equal(t, []string{want[name]}, enforcer.checked, name)
	}
}

func TestListRevisionsParsesFilters(t *testing.T) {
	t.Parallel()

	svc := &fakeRevisions{}
	h := adminListPostRevisionsHandler(svc, always(true))
	postID := uuid.New()
	author := uuid.New()

	w, body := runProfile(t, h, profileRequest{
		method: nethttp.MethodGet, param: postID.String(), user: &testUserID,
		target: "/?authorId=" + author.String() + "&fromDate=2026-09-01&toDate=2026-09-02&includeDiff=false&page=2&perPage=5",
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, postID, svc.filter.PostUUID)
	assert.Equal(t, &author, svc.filter.AuthorUUID)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), *svc.filter.From)
	assert.Equal(t, time.Date(2026, 9, 2, 23, 59, 59, 999999999, time.UTC), *svc.filter.To, "a toDate covers the whole day")
	assert.Equal(t, 2, svc.filter.Page)
	assert.Equal(t, 5, svc.filter.PerPage)
	assert.True(t, svc.unrestricted)

	items, _ := body["data"].([]any)
	require.Len(t, items, 1)
	first, _ := items[0].(map[string]any)
	assert.Equal(t, "Updated title", first["changelog"])
	assert.NotContains(t, first, "diff", "includeDiff=false drops diffs")
	assert.NotContains(t, first, "snapshot")

	for _, bad := range []string{"authorId=x", "fromDate=yesterday", "toDate=2026-13-01", "includeDiff=maybe"} {
		w, body := runProfile(t, h, profileRequest{method: nethttp.MethodGet, param: postID.String(), user: &testUserID, target: "/?" + bad})
		require.Equal(t, nethttp.StatusBadRequest, w.Code, bad)
		assert.Equal(t, responses.ErrorCodeValidation, errorCode(body), bad)
	}
}

func TestGetRevisionByNumberOrUUID(t *testing.T) {
	t.Parallel()

	svc := &fakeRevisions{}
	h := adminGetPostRevisionHandler(svc, func(*gin.Context) bool { return false })
	postID := uuid.New().String()

	w, body := runProfile(t, h, profileRequest{method: nethttp.MethodGet, target: "/", param: postID, param2: "3", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, svc.ref.Number)
	assert.Equal(t, 3, *svc.ref.Number)
	assert.False(t, svc.unrestricted)

	data := dataOf(body)
	snapshot, _ := data["snapshot"].(map[string]any)
	assert.Equal(t, "full text", snapshot["content"])
	assert.Contains(t, data, "diff")

	w, body = runProfile(t, h, profileRequest{method: nethttp.MethodGet, target: "/", param: postID, param2: "0", user: &testUserID})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	assert.Equal(t, responses.ErrorCodeValidation, errorCode(body))

	svc.err = postdomain.ErrRevisionNotFound
	w, body = runProfile(t, h, profileRequest{method: nethttp.MethodGet, target: "/", param: postID, param2: uuid.NewString(), user: &testUserID})
	require.Equal(t, nethttp.StatusNotFound, w.Code)

	errObj, _ := body["error"].(map[string]any)
	assert.Equal(t, "Revision not found", errObj["message"])
}

func TestRestoreRevisionPassesNoteAndEditor(t *testing.T) {
	t.Parallel()

	svc := &fakeRevisions{}
	h := withPostEditor(adminRestorePostRevisionHandler(svc, always(true)))
	postID := uuid.New().String()

	w, body := runProfile(t, h, profileRequest{
		method: nethttp.MethodPost, target: "/", param: postID, param2: "1", user: &testUserID,
		contentType: "application/json", body: `{"restoreNote":"  undo  "}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "undo", svc.note)
	assert.Equal(t, &testUserID, svc.editor.UserID)

	data := dataOf(body)
	post, _ := data["post"].(map[string]any)
	assert.Equal(t, "Restored", post["title"])
	assert.Contains(t, data, "revision")
	assert.Contains(t, data, "skipped")

	w, _ = runProfile(t, h, profileRequest{method: nethttp.MethodPost, target: "/", param: postID, param2: "1", user: &testUserID})
	require.Equal(t, nethttp.StatusOK, w.Code, "the body is optional")

	svc.err = postdomain.ErrStaleVersion
	w, body = runProfile(t, h, profileRequest{method: nethttp.MethodPost, target: "/", param: postID, param2: "1", user: &testUserID})
	require.Equal(t, nethttp.StatusConflict, w.Code)
	assert.Equal(t, "post.version_conflict", errorCode(body))
}
