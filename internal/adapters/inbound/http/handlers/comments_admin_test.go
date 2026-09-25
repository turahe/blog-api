package handlers

import (
	"context"
	"fmt"
	nethttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
)

type fakeModerationService struct {
	listFn       func(context.Context, commentservice.AdminListInput) (commentdomain.ListResult, error)
	getFn        func(context.Context, uuid.UUID) (commentdomain.Review, error)
	moderateFn   func(context.Context, commentservice.ModerateInput) (commentdomain.Comment, error)
	bulkFn       func(context.Context, commentservice.BulkModerateInput) (int, error)
	hardDeleteFn func(context.Context, uuid.UUID, uuid.UUID, string) (bool, error)
	statsFn      func(context.Context) (commentdomain.Stats, error)
}

func (f *fakeModerationService) AdminList(ctx context.Context, in commentservice.AdminListInput) (commentdomain.ListResult, error) {
	return f.listFn(ctx, in)
}

func (f *fakeModerationService) AdminGet(ctx context.Context, id uuid.UUID) (commentdomain.Review, error) {
	return f.getFn(ctx, id)
}

func (f *fakeModerationService) Moderate(ctx context.Context, in commentservice.ModerateInput) (commentdomain.Comment, error) {
	return f.moderateFn(ctx, in)
}

func (f *fakeModerationService) BulkModerate(ctx context.Context, in commentservice.BulkModerateInput) (int, error) {
	return f.bulkFn(ctx, in)
}

func (f *fakeModerationService) HardDelete(ctx context.Context, moderatorID, id uuid.UUID, reason string) (bool, error) {
	return f.hardDeleteFn(ctx, moderatorID, id, reason)
}

func (f *fakeModerationService) Stats(ctx context.Context) (commentdomain.Stats, error) {
	return f.statsFn(ctx)
}

type recordingEnforcer struct {
	allow   bool
	checked []string
}

func (e *recordingEnforcer) Enforce(_ context.Context, _ uuid.UUID, permission string) (bool, error) {
	e.checked = append(e.checked, permission)
	return e.allow, nil
}

func adminCommentHandlers(deps Deps) map[string]gin.HandlerFunc {
	deps.Comments = commentservice.New(nil, nil, nil, commentservice.Config{})
	c := NewControllers(deps).Comments

	return map[string]gin.HandlerFunc{
		"list":       c.AdminList,
		"get":        c.AdminGet,
		"stats":      c.AdminStats,
		"moderate":   c.AdminModerate,
		"bulk":       c.AdminBulkModerate,
		"hardDelete": c.AdminHardDelete,
	}
}

func TestAdminCommentRoutesRequirePermissions(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"list": "comment.moderate", "get": "comment.moderate", "stats": "comment.moderate",
		"moderate": "comment.moderate", "bulk": "comment.moderate", "hardDelete": "comment.delete",
	}

	enforcer := &recordingEnforcer{}
	for name, handler := range adminCommentHandlers(Deps{RBAC: enforcer}) {
		enforcer.checked = nil
		c, w := commentContext(nethttp.MethodPost, "/", `{}`, &testUserID, testCommentID.String())

		handler(c)

		require.Equal(t, nethttp.StatusForbidden, w.Code, name)
		require.Equal(t, "rbac.forbidden", decodeEnvelope(t, w).Error.Code, name)
		require.Equal(t, []string{want[name]}, enforcer.checked, name)
	}

	for name, handler := range adminCommentHandlers(Deps{RBAC: enforcer}) {
		c, w := commentContext(nethttp.MethodPost, "/", `{}`, nil, testCommentID.String())

		handler(c)

		require.Equal(t, nethttp.StatusUnauthorized, w.Code, name)
	}
}

func TestAdminCommentRoleFallback(t *testing.T) {
	t.Parallel()

	editor := adminCommentHandlers(Deps{Roles: fakeRoleLookup{names: []string{"editor"}}})
	c, w := commentContext(nethttp.MethodDelete, "/", "", &testUserID, testCommentID.String())
	editor["hardDelete"](c)
	require.Equal(t, nethttp.StatusForbidden, w.Code, "editors cannot hard delete")

	author := adminCommentHandlers(Deps{Roles: fakeRoleLookup{names: []string{"author"}}})
	for name, handler := range author {
		c, w := commentContext(nethttp.MethodPost, "/", `{}`, &testUserID, testCommentID.String())
		handler(c)
		require.Equal(t, nethttp.StatusForbidden, w.Code, name)
	}
}

func TestAdminModerateCommentPassesInputAndShowsIdentity(t *testing.T) {
	t.Parallel()

	var got commentservice.ModerateInput

	svc := &fakeModerationService{moderateFn: func(_ context.Context, in commentservice.ModerateInput) (commentdomain.Comment, error) {
		got = in

		return commentdomain.Comment{
			UUID: in.CommentUUID, PostUUID: testPostID, AuthorName: "Guest", AuthorEmail: "guest@example.com",
			IPHash: "abc", Content: "hi", Status: commentdomain.StatusRejected, ModeratedByUUID: &in.ModeratorUUID,
			ModerationReason: in.Reason, CreatedAt: testTime, UpdatedAt: testTime,
		}, nil
	}}
	c, w := commentContext(nethttp.MethodPost, "/", `{"action":"reject","reason":"off topic","notifyAuthor":true}`,
		&testUserID, testCommentID.String())

	adminModerateCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, commentservice.ModerateInput{
		ModeratorUUID: testUserID, CommentUUID: testCommentID, Action: commentdomain.ActionReject,
		Reason: "off topic", NotifyAuthor: true,
	}, got)

	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	require.Equal(t, "rejected", data["status"])
	require.Equal(t, "guest@example.com", data["authorEmail"])
	require.Equal(t, "abc", data["ipHash"])
	require.Equal(t, testUserID.String(), data["moderatedBy"])
	require.Equal(t, "off topic", data["moderationReason"])
}

func TestAdminModerateCommentRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	c, w := commentContext(nethttp.MethodPost, "/", `{"action":"hardDelete"}`, &testUserID, testCommentID.String())

	adminModerateCommentHandler(&fakeModerationService{})(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestAdminModerateCommentInvalidTransitionIsConflict(t *testing.T) {
	t.Parallel()

	svc := &fakeModerationService{moderateFn: func(context.Context, commentservice.ModerateInput) (commentdomain.Comment, error) {
		return commentdomain.Comment{}, fmt.Errorf("%w: cannot restore a pending comment", commentdomain.ErrInvalidTransition)
	}}
	c, w := commentContext(nethttp.MethodPost, "/", `{"action":"restore"}`, &testUserID, testCommentID.String())

	adminModerateCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	require.Equal(t, "comment.invalid_transition", decodeEnvelope(t, w).Error.Code)
}

func TestAdminBulkModerateReportsBlockingIDs(t *testing.T) {
	t.Parallel()

	blocked := uuid.New()
	cases := map[error]struct {
		status int
		code   string
	}{
		commentdomain.ErrInvalidTransition: {nethttp.StatusConflict, "comment.invalid_transition"},
		commentdomain.ErrNotFound:          {nethttp.StatusNotFound, "not_found"},
	}

	for cause, want := range cases {
		svc := &fakeModerationService{bulkFn: func(_ context.Context, in commentservice.BulkModerateInput) (int, error) {
			require.Equal(t, testUserID, in.ModeratorUUID)
			require.Equal(t, []uuid.UUID{testCommentID, blocked}, in.CommentUUIDs)

			return 0, &commentdomain.BatchError{Err: cause, IDs: []uuid.UUID{blocked}}
		}}
		body := fmt.Sprintf(`{"ids":[%q,%q],"action":"spam"}`, testCommentID, blocked)
		c, w := commentContext(nethttp.MethodPost, "/", body, &testUserID, "")

		adminBulkModerateCommentsHandler(svc)(c)

		require.Equal(t, want.status, w.Code, cause.Error())
		envelope := decodeEnvelope(t, w)
		require.Equal(t, want.code, envelope.Error.Code)
		require.Equal(t, []any{blocked.String()}, as[map[string]any](t, envelope.Error.Details)["ids"])
	}
}

func TestAdminBulkModerateSucceeds(t *testing.T) {
	t.Parallel()

	svc := &fakeModerationService{bulkFn: func(_ context.Context, in commentservice.BulkModerateInput) (int, error) {
		return len(in.CommentUUIDs), nil
	}}
	c, w := commentContext(nethttp.MethodPost, "/", fmt.Sprintf(`{"ids":[%q],"action":"approve"}`, testCommentID), &testUserID, "")

	adminBulkModerateCommentsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	require.InDelta(t, 1, as[map[string]any](t, decodeEnvelope(t, w).Data)["updated"], 0)
}

func TestAdminBulkModerateValidatesBody(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"ids":[],"action":"approve"}`,
		`{"ids":["nope"],"action":"approve"}`,
		fmt.Sprintf(`{"ids":[%q],"action":"delete"}`, testCommentID),
	} {
		c, w := commentContext(nethttp.MethodPost, "/", body, &testUserID, "")
		adminBulkModerateCommentsHandler(&fakeModerationService{})(c)
		require.Equal(t, nethttp.StatusBadRequest, w.Code, body)
	}
}

func TestAdminHardDeleteReportsOutcome(t *testing.T) {
	t.Parallel()

	svc := &fakeModerationService{hardDeleteFn: func(_ context.Context, moderatorID, id uuid.UUID, reason string) (bool, error) {
		require.Equal(t, testUserID, moderatorID)
		require.Equal(t, testCommentID, id)
		require.Equal(t, "doxxing", reason)

		return true, nil
	}}
	c, w := commentContext(nethttp.MethodDelete, "/?reason=doxxing", "", &testUserID, testCommentID.String())

	adminHardDeleteCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	require.Equal(t, "scrubbed", data["outcome"])
}

func TestAdminListCommentsParsesFilters(t *testing.T) {
	t.Parallel()

	var got commentservice.AdminListInput

	svc := &fakeModerationService{listFn: func(_ context.Context, in commentservice.AdminListInput) (commentdomain.ListResult, error) {
		got = in

		return commentdomain.ListResult{Items: []commentdomain.Comment{{
			UUID: testCommentID, PostUUID: testPostID, Content: "kept", Status: commentdomain.StatusDeleted,
			CreatedAt: testTime, UpdatedAt: testTime,
		}}, Page: 1, PerPage: 20, Total: 1}, nil
	}}
	c, w := commentContext(nethttp.MethodGet,
		"/?status=spam,+rejected&status=deleted&sort=newest&postId="+testPostID.String(), "", &testUserID, "")

	adminListCommentsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, []commentdomain.Status{
		commentdomain.StatusSpam, commentdomain.StatusRejected, commentdomain.StatusDeleted,
	}, got.Statuses)
	require.Equal(t, testPostID, *got.PostUUID)
	require.True(t, got.NewestFirst)

	items := as[[]any](t, decodeEnvelope(t, w).Data)
	require.Equal(t, "kept", as[map[string]any](t, items[0])["content"])

	c, w = commentContext(nethttp.MethodGet, "/?sort=random", "", &testUserID, "")
	adminListCommentsHandler(svc)(c)
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestAdminGetCommentIncludesFlagsAndLog(t *testing.T) {
	t.Parallel()

	svc := &fakeModerationService{getFn: func(_ context.Context, id uuid.UUID) (commentdomain.Review, error) {
		return commentdomain.Review{
			Comment: commentdomain.Comment{UUID: id, PostUUID: testPostID, Status: commentdomain.StatusFlagged, CreatedAt: testTime, UpdatedAt: testTime},
			Flags:   []commentdomain.Flag{{CommentUUID: id, Reason: "spam", CreatedAt: testTime}},
			History: []commentdomain.ModerationEntry{{
				UUID: uuid.New(), CommentUUID: id, Action: commentdomain.ActionReject,
				FromStatus: commentdomain.StatusFlagged, ToStatus: commentdomain.StatusRejected,
				Before: map[string]any{"status": "flagged"}, CreatedAt: testTime,
			}},
		}, nil
	}}
	c, w := commentContext(nethttp.MethodGet, "/", "", &testUserID, testCommentID.String())

	adminGetCommentHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	flag := as[map[string]any](t, as[[]any](t, data["flags"])[0])
	require.Equal(t, true, flag["guest"])
	entry := as[map[string]any](t, as[[]any](t, data["moderationLog"])[0])
	require.Equal(t, "reject", entry["action"])
	require.Equal(t, map[string]any{"status": "flagged"}, entry["before"])
}

func TestAdminCommentStatsSerializesAllStatuses(t *testing.T) {
	t.Parallel()

	svc := &fakeModerationService{statsFn: func(context.Context) (commentdomain.Stats, error) {
		return commentdomain.Stats{
			ByStatus:   map[commentdomain.Status]int64{commentdomain.StatusPending: 2},
			QueueDepth: 2,
			TopPosts:   []commentdomain.PostQueue{{PostUUID: testPostID, PostTitle: "Hello", Pending: 2}},
		}, nil
	}}
	c, w := commentContext(nethttp.MethodGet, "/", "", &testUserID, "")

	adminCommentStatsHandler(svc)(c)

	require.Equal(t, nethttp.StatusOK, w.Code)
	data := as[map[string]any](t, decodeEnvelope(t, w).Data)
	byStatus := as[map[string]any](t, data["byStatus"])
	require.Len(t, byStatus, 6)
	require.InDelta(t, 2, byStatus["pending"], 0)
	require.Nil(t, data["oldestQueuedAt"])
}
