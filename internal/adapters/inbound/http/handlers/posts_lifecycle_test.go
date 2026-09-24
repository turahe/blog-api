package handlers

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type fakePostLifecycle struct {
	calls []string
	err   error
}

func (f *fakePostLifecycle) apply(name string, id uuid.UUID, status postdomain.Status) (postdomain.Post, error) {
	f.calls = append(f.calls, name)
	if f.err != nil {
		return postdomain.Post{}, f.err
	}

	return postdomain.Post{UUID: id, Slug: "hello", Status: status, Version: 2}, nil
}

func (f *fakePostLifecycle) Publish(_ context.Context, id uuid.UUID) (postdomain.Post, error) {
	return f.apply("publish", id, postdomain.StatusPublished)
}

func (f *fakePostLifecycle) Unpublish(_ context.Context, id uuid.UUID) (postdomain.Post, error) {
	return f.apply("unpublish", id, postdomain.StatusDraft)
}

func (f *fakePostLifecycle) Archive(_ context.Context, id uuid.UUID) (postdomain.Post, error) {
	return f.apply("archive", id, postdomain.StatusArchived)
}

func (f *fakePostLifecycle) Restore(_ context.Context, id uuid.UUID) (postdomain.Post, error) {
	return f.apply("restore", id, postdomain.StatusDraft)
}

func (f *fakePostLifecycle) Delete(_ context.Context, id uuid.UUID) error {
	_, err := f.apply("delete", id, "")
	return err
}

func runPostLifecycle(t *testing.T, handler gin.HandlerFunc, method, id string) (*httptest.ResponseRecorder, responses.Envelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: id}}
	c.Request = httptest.NewRequestWithContext(t.Context(), method, "/api/v1/admin/posts/"+id, nil)

	handler(c)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))

	return w, envelope
}

func TestAdminPostTransitionHandlersReturnUpdatedPost(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		build  func(postLifecycleAPI) gin.HandlerFunc
		status string
	}{
		"publish":   {adminPublishPostHandler, "published"},
		"unpublish": {adminUnpublishPostHandler, "draft"},
		"archive":   {adminArchivePostHandler, "archived"},
		"restore":   {adminRestorePostHandler, "draft"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := &fakePostLifecycle{}
			id := uuid.New()

			w, envelope := runPostLifecycle(t, tc.build(svc), nethttp.MethodPost, id.String())

			require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
			require.Equal(t, []string{name}, svc.calls)
			data := as[map[string]any](t, envelope.Data)
			require.Equal(t, id.String(), data["id"])
			require.Equal(t, tc.status, data["status"])
		})
	}
}

func TestAdminDeletePostHandler(t *testing.T) {
	t.Parallel()

	svc := &fakePostLifecycle{}
	id := uuid.New()

	w, envelope := runPostLifecycle(t, adminDeletePostHandler(svc), nethttp.MethodDelete, id.String())

	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"delete"}, svc.calls)
	data := as[map[string]any](t, envelope.Data)
	require.Equal(t, id.String(), data["id"])
	require.Equal(t, true, data["deleted"])
}

func TestAdminPostLifecycleHandlersMapErrors(t *testing.T) {
	t.Parallel()

	invalid, err := postdomain.TransitionUnpublish.Next(postdomain.StatusDraft)
	require.Empty(t, invalid)

	cases := map[string]struct {
		err     error
		status  int
		code    string
		message string
	}{
		"invalid transition": {err, nethttp.StatusConflict, "post.invalid_transition", "invalid status transition: cannot unpublish a draft post"},
		"not found":          {postdomain.ErrNotFound, nethttp.StatusNotFound, "not_found", ""},
		"stale version":      {postdomain.ErrStaleVersion, nethttp.StatusConflict, "post.version_conflict", ""},
		"slug conflict":      {postdomain.ErrConflict, nethttp.StatusConflict, "conflict", "Post conflict"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := &fakePostLifecycle{err: tc.err}

			w, envelope := runPostLifecycle(t, adminUnpublishPostHandler(svc), nethttp.MethodPost, uuid.NewString())

			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.False(t, envelope.OK)
			require.Equal(t, tc.code, envelope.Error.Code)

			if tc.message != "" {
				require.Equal(t, tc.message, envelope.Error.Message)
			}
		})
	}
}

func TestAdminPostLifecycleHandlersRejectBadID(t *testing.T) {
	t.Parallel()

	svc := &fakePostLifecycle{}

	w, envelope := runPostLifecycle(t, adminDeletePostHandler(svc), nethttp.MethodDelete, "not-a-uuid")

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "validation_error", envelope.Error.Code)
	require.Empty(t, svc.calls)
}
