package handlers

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
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
)

type fakeCommentService struct {
	createFn func(context.Context, commentservice.CreateInput) (commentdomain.Comment, error)
	listFn   func(context.Context, uuid.UUID, *uuid.UUID, int, int) (commentdomain.ListResult, error)
	threadFn func(context.Context, uuid.UUID) (commentdomain.Thread, error)
	mineFn   func(context.Context, uuid.UUID, int, int) (commentdomain.ListResult, error)
	updateFn func(context.Context, uuid.UUID, uuid.UUID, string) (commentdomain.Comment, error)
	deleteFn func(context.Context, uuid.UUID, uuid.UUID) error
	flagFn   func(context.Context, commentservice.FlagInput) error
	upvoteFn func(context.Context, uuid.UUID, uuid.UUID) (bool, int, error)
}

func (f *fakeCommentService) Create(ctx context.Context, in commentservice.CreateInput) (commentdomain.Comment, error) {
	return f.createFn(ctx, in)
}

func (f *fakeCommentService) ListForPost(ctx context.Context, postID uuid.UUID, parentID *uuid.UUID, page, perPage int) (commentdomain.ListResult, error) {
	return f.listFn(ctx, postID, parentID, page, perPage)
}

func (f *fakeCommentService) GetThread(ctx context.Context, id uuid.UUID) (commentdomain.Thread, error) {
	return f.threadFn(ctx, id)
}

func (f *fakeCommentService) ListMine(ctx context.Context, userID uuid.UUID, page, perPage int) (commentdomain.ListResult, error) {
	return f.mineFn(ctx, userID, page, perPage)
}

func (f *fakeCommentService) Update(ctx context.Context, actorID, id uuid.UUID, content string) (commentdomain.Comment, error) {
	return f.updateFn(ctx, actorID, id, content)
}

func (f *fakeCommentService) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	return f.deleteFn(ctx, actorID, id)
}

func (f *fakeCommentService) Flag(ctx context.Context, in commentservice.FlagInput) error {
	return f.flagFn(ctx, in)
}

func (f *fakeCommentService) ToggleUpvote(ctx context.Context, voterID, id uuid.UUID) (bool, int, error) {
	return f.upvoteFn(ctx, voterID, id)
}

var (
	testPostID    = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	testCommentID = uuid.MustParse("cccccccc-0000-0000-0000-000000000001")
	testUserID    = uuid.MustParse("11111111-0000-0000-0000-000000000001")
	testTime      = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
)

func commentContext(method, target, body string, user *uuid.UUID, param string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	if param != "" {
		c.Params = gin.Params{{Key: "param1", Value: param}}
	}

	if user != nil {
		c.Set(middleware.ContextUserIDKey, *user)
	}

	return c, w
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) responses.Envelope {
	t.Helper()

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))

	return envelope
}

func as[T any](t *testing.T, v any) T {
	t.Helper()

	typed, ok := v.(T)
	require.True(t, ok, "unexpected type %T", v)

	return typed
}

func TestCreateCommentPassesSignedInAuthorAndMasksSpam(t *testing.T) {
	t.Parallel()

	var got commentservice.CreateInput

	svc := &fakeCommentService{createFn: func(_ context.Context, in commentservice.CreateInput) (commentdomain.Comment, error) {
		got = in

		return commentdomain.Comment{
			UUID: testCommentID, PostUUID: in.PostUUID, AuthorUUID: in.AuthorUUID, Content: in.Content,
			Status: commentdomain.StatusSpam, CreatedAt: testTime, UpdatedAt: testTime,
		}, nil
	}}
	c, w := commentContext(nethttp.MethodPost, "/api/v1/posts/x/comments",
		`{"content":"hi","honeypot":"bot","authorName":"Ignored","authorEmail":"x@example.com"}`, &testUserID, testPostID.String())

	createPostCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)
	require.Equal(t, testUserID, *got.AuthorUUID)
	require.Equal(t, "bot", got.Honeypot)

	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	require.Equal(t, "pending", data["status"])
	require.Equal(t, 2010301, decodeEnvelope(t, w).Code)
}

func TestCreateCommentGuestDisabledIsUnauthorized(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{createFn: func(_ context.Context, in commentservice.CreateInput) (commentdomain.Comment, error) {
		require.Nil(t, in.AuthorUUID)
		return commentdomain.Comment{}, commentdomain.ErrGuestDisabled
	}}
	c, w := commentContext(nethttp.MethodPost, "/api/v1/posts/x/comments", `{"content":"hi"}`, nil, testPostID.String())

	createPostCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusUnauthorized, w.Code)
	require.Equal(t, "unauthorized", decodeEnvelope(t, w).Error.Code)
}

func TestCreateCommentValidatesBody(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{}
	c, w := commentContext(nethttp.MethodPost, "/api/v1/posts/x/comments", `{"content":"","parentId":"nope"}`, &testUserID, testPostID.String())

	createPostCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	details := as[map[string]any](t, decodeEnvelope(t, w).Error.Details)
	require.Contains(t, details, "content")
	require.Contains(t, details, "parentId")
}

func TestPatchCommentByNonOwnerIsForbidden(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{updateFn: func(_ context.Context, actorID, id uuid.UUID, _ string) (commentdomain.Comment, error) {
		require.Equal(t, testUserID, actorID)
		require.Equal(t, testCommentID, id)

		return commentdomain.Comment{}, commentdomain.ErrForbidden
	}}
	c, w := commentContext(nethttp.MethodPatch, "/api/v1/comments/x", `{"content":"hijack"}`, &testUserID, testCommentID.String())

	patchCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusForbidden, w.Code)
	require.Equal(t, "forbidden", decodeEnvelope(t, w).Error.Code)
}

func TestPatchCommentAfterEditWindow(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{updateFn: func(context.Context, uuid.UUID, uuid.UUID, string) (commentdomain.Comment, error) {
		return commentdomain.Comment{}, commentdomain.ErrEditWindowClosed
	}}
	c, w := commentContext(nethttp.MethodPatch, "/api/v1/comments/x", `{"content":"late"}`, &testUserID, testCommentID.String())

	patchCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusForbidden, w.Code)
	require.Equal(t, "comment.edit_window_closed", decodeEnvelope(t, w).Error.Code)
}

func TestCommentPolicyErrors(t *testing.T) {
	t.Parallel()

	for code, policyErr := range map[string]error{
		"comment.disabled": commentdomain.ErrCommentsDisabled,
		"comment.closed":   commentdomain.ErrCommentsClosed,
	} {
		svc := &fakeCommentService{listFn: func(context.Context, uuid.UUID, *uuid.UUID, int, int) (commentdomain.ListResult, error) {
			return commentdomain.ListResult{}, policyErr
		}}
		c, w := commentContext(nethttp.MethodGet, "/api/v1/posts/x/comments", "", nil, testPostID.String())

		listPostCommentsHandler(svc)(c)

		require.Equal(t, nethttp.StatusForbidden, w.Code, code)
		require.Equal(t, code, decodeEnvelope(t, w).Error.Code)
	}
}

func TestCreateCommentCaptcha(t *testing.T) {
	t.Parallel()

	var got commentservice.CreateInput

	for status, captchaErr := range map[int]error{
		nethttp.StatusBadRequest:         commentdomain.ErrChallengeFailed,
		nethttp.StatusServiceUnavailable: commentdomain.ErrChallengeUnavailable,
	} {
		svc := &fakeCommentService{createFn: func(_ context.Context, in commentservice.CreateInput) (commentdomain.Comment, error) {
			got = in
			return commentdomain.Comment{}, captchaErr
		}}
		c, w := commentContext(nethttp.MethodPost, "/api/v1/posts/x/comments",
			`{"content":"hi","authorName":"Ann","authorEmail":"ann@example.com","turnstileResponse":"tok"}`, nil, testPostID.String())

		createPostCommentHandler(svc)(c)

		require.Equal(t, status, w.Code)
		require.Equal(t, "tok", got.CaptchaToken)
	}
}

func TestDeleteCommentByNonOwnerIsForbidden(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return commentdomain.ErrForbidden
	}}
	c, w := commentContext(nethttp.MethodDelete, "/api/v1/comments/x", "", &testUserID, testCommentID.String())

	deleteCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusForbidden, w.Code)
}

func TestSelfCommentHandlersRequireIdentity(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{}

	handlers := map[string]gin.HandlerFunc{
		"list":   listMyCommentsHandler(svc),
		"patch":  patchCommentHandler(svc),
		"delete": deleteCommentHandler(svc),
		"upvote": upvoteCommentHandler(svc),
	}
	for name, handler := range handlers {
		c, w := commentContext(nethttp.MethodPost, "/api/v1/comments/x", `{"content":"x"}`, nil, testCommentID.String())
		handler(c)
		require.Equal(t, nethttp.StatusUnauthorized, w.Code, name)
	}
}

func TestGetCommentHidesDeletedContentAndAuthor(t *testing.T) {
	t.Parallel()

	deletedAt := testTime
	svc := &fakeCommentService{threadFn: func(context.Context, uuid.UUID) (commentdomain.Thread, error) {
		return commentdomain.Thread{
			Comment: commentdomain.Comment{
				UUID: testCommentID, PostUUID: testPostID, AuthorUUID: &testUserID, AuthorUsername: "ann",
				AuthorEmail: "ann@example.com", IPHash: "abc", Content: "secret", ContentHTML: "<p>secret</p>",
				Status: commentdomain.StatusDeleted, DeletedAt: &deletedAt, CreatedAt: testTime, UpdatedAt: testTime,
			},
			Replies: commentdomain.ListResult{Items: []commentdomain.Comment{{
				UUID: uuid.New(), PostUUID: testPostID, ParentUUID: &testCommentID, AuthorName: "Guest",
				AuthorEmail: "guest@example.com", Content: "reply", ContentHTML: "<p>reply</p>", Status: commentdomain.StatusApproved, Depth: 1,
				CreatedAt: testTime, UpdatedAt: testTime,
			}}, Total: 1},
		}, nil
	}}
	c, w := commentContext(nethttp.MethodGet, "/api/v1/comments/x", "", nil, testCommentID.String())

	getCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "secret")
	require.NotContains(t, w.Body.String(), "@example.com")

	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	require.Empty(t, data["content"])
	require.Empty(t, data["contentHtml"])
	require.Nil(t, data["author"])

	replies := as[[]any](t, data["replies"])
	require.Len(t, replies, 1)

	reply := as[map[string]any](t, replies[0])
	require.Equal(t, map[string]any{"id": nil, "name": "Guest", "guest": true}, reply["author"])
	require.Equal(t, testCommentID.String(), reply["parentId"])
	require.Equal(t, "<p>reply</p>", reply["contentHtml"])
}

func TestListPostCommentsPaginatesAndParsesParent(t *testing.T) {
	t.Parallel()

	parent := uuid.New()
	svc := &fakeCommentService{listFn: func(_ context.Context, postID uuid.UUID, parentID *uuid.UUID, page, perPage int) (commentdomain.ListResult, error) {
		require.Equal(t, testPostID, postID)
		require.Equal(t, parent, *parentID)
		require.Equal(t, 2, page)
		require.Equal(t, 5, perPage)

		return commentdomain.ListResult{Page: 2, PerPage: 5, Total: 7}, nil
	}}
	c, w := commentContext(nethttp.MethodGet, "/api/v1/posts/x/comments?page=2&perPage=5&parentId="+parent.String(), "", nil, testPostID.String())

	listPostCommentsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	envelope := decodeEnvelope(t, w)
	require.NotNil(t, envelope.Links)
	require.InDelta(t, 7, as[map[string]any](t, envelope.Meta)["total"], 0)
}

func TestListPostCommentsUnknownPost(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{listFn: func(context.Context, uuid.UUID, *uuid.UUID, int, int) (commentdomain.ListResult, error) {
		return commentdomain.ListResult{}, commentdomain.ErrPostNotFound
	}}
	c, w := commentContext(nethttp.MethodGet, "/api/v1/posts/x/comments", "", nil, testPostID.String())

	listPostCommentsHandler(svc)(c)

	require.Equal(t, nethttp.StatusNotFound, w.Code)
}

func TestFlagCommentAsGuestIsAccepted(t *testing.T) {
	t.Parallel()

	var got commentservice.FlagInput

	svc := &fakeCommentService{flagFn: func(_ context.Context, in commentservice.FlagInput) error {
		got = in
		return nil
	}}
	c, w := commentContext(nethttp.MethodPost, "/api/v1/comments/x/flag", `{"reasonCode":"spam"}`, nil, testCommentID.String())

	flagCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusAccepted, w.Code)
	require.Nil(t, got.ReporterUUID)
	require.Equal(t, "spam", got.Reason)
	require.NotEmpty(t, got.ClientIP)
}

func TestFlagCommentRejectsUnknownReason(t *testing.T) {
	t.Parallel()

	c, w := commentContext(nethttp.MethodPost, "/api/v1/comments/x/flag", `{"reasonCode":"boring"}`, nil, testCommentID.String())

	flagCommentHandler(&fakeCommentService{})(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestUpvoteCommentReturnsState(t *testing.T) {
	t.Parallel()

	svc := &fakeCommentService{upvoteFn: func(_ context.Context, voterID, _ uuid.UUID) (bool, int, error) {
		require.Equal(t, testUserID, voterID)
		return true, 4, nil
	}}
	c, w := commentContext(nethttp.MethodPost, "/api/v1/comments/x/upvote", "", &testUserID, testCommentID.String())

	upvoteCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)

	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	require.Equal(t, true, data["upvoted"])
	require.InDelta(t, 4, data["upvoteCount"], 0)
}

func TestMapCommentErrorCodes(t *testing.T) {
	t.Parallel()

	cases := map[error]struct {
		status int
		code   string
	}{
		commentdomain.ErrValidation:    {nethttp.StatusBadRequest, "validation_error"},
		commentdomain.ErrNotFound:      {nethttp.StatusNotFound, "not_found"},
		commentdomain.ErrNotEditable:   {nethttp.StatusConflict, "comment.not_editable"},
		commentdomain.ErrParentInvalid: {nethttp.StatusUnprocessableEntity, "comment.parent_invalid"},
		commentdomain.ErrDepthExceeded: {nethttp.StatusUnprocessableEntity, "comment.depth_exceeded"},
		context.Canceled:               {nethttp.StatusInternalServerError, "internal_error"},
	}
	for err, want := range cases {
		c, w := commentContext(nethttp.MethodGet, "/", "", nil, "")
		require.True(t, mapCommentError(c, err))
		require.Equal(t, want.status, w.Code, err.Error())
		require.Equal(t, want.code, decodeEnvelope(t, w).Error.Code, err.Error())
	}
}
