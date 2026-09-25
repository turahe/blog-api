package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

type fakeInbox struct {
	userID     uuid.UUID
	unreadOnly bool
	page       int
	perPage    int
	result     notificationdomain.ListResult
	read       notificationdomain.Notification
	err        error
}

func (f *fakeInbox) List(_ context.Context, userID uuid.UUID, unreadOnly bool, page, perPage int) (notificationdomain.ListResult, error) {
	f.userID, f.unreadOnly, f.page, f.perPage = userID, unreadOnly, page, perPage
	return f.result, f.err
}

func (f *fakeInbox) MarkRead(_ context.Context, userID, _ uuid.UUID) (notificationdomain.Notification, error) {
	f.userID = userID
	return f.read, f.err
}

func sampleNotification() notificationdomain.Notification {
	return notificationdomain.Notification{
		UUID:      uuid.New(),
		Type:      "comment.reply",
		Title:     "Grace replied to your comment",
		Body:      `On "Hello": nice`,
		Preview:   "nice",
		Payload:   map[string]string{"post_id": uuid.NewString()},
		CreatedAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
	}
}

func TestMeNotificationsList(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	inbox := &fakeInbox{result: notificationdomain.ListResult{
		Items: []notificationdomain.Notification{sampleNotification()}, Total: 1, Unread: 4, Page: 2, PerPage: 5,
	}}

	w, body := runProfile(t, meNotificationsListHandler(inbox), profileRequest{
		method: nethttp.MethodGet, target: "/me/notifications?unread=true&page=2&per_page=5", user: &user,
	})

	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "4", w.Header().Get(headerUnreadCount))
	require.Equal(t, user, inbox.userID)
	require.True(t, inbox.unreadOnly)
	require.Equal(t, 2, inbox.page)
	require.Equal(t, 5, inbox.perPage)

	items, _ := body["data"].([]any)
	require.Len(t, items, 1)
	item, _ := items[0].(map[string]any)
	require.Equal(t, "comment.reply", item["type"])
	require.Equal(t, false, item["is_read"])
	require.Contains(t, item, "data")
}

func TestMeNotificationsListRejectsBadUnread(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	w, body := runProfile(t, meNotificationsListHandler(&fakeInbox{}), profileRequest{
		method: nethttp.MethodGet, target: "/me/notifications?unread=maybe", user: &user,
	})

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "validation_error", errorCode(body))
}

func TestMeNotificationRead(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	readAt := time.Date(2026, 9, 25, 11, 0, 0, 0, time.UTC)
	read := sampleNotification()
	read.ReadAt = &readAt

	for name, tc := range map[string]struct {
		param string
		err   error
		code  int
		ec    string
	}{
		"marks read":     {param: read.UUID.String(), code: nethttp.StatusOK},
		"invalid id":     {param: "nope", code: nethttp.StatusBadRequest, ec: "validation_error"},
		"someone else's": {param: uuid.NewString(), err: notificationdomain.ErrNotFound, code: nethttp.StatusNotFound, ec: "not_found"},
		"store failure":  {param: uuid.NewString(), err: errors.New("db down"), code: nethttp.StatusInternalServerError, ec: "internal_error"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inbox := &fakeInbox{read: read, err: tc.err}

			w, body := runProfile(t, meNotificationReadHandler(inbox), profileRequest{
				method: nethttp.MethodPost, target: "/me/notifications/x/read", param: tc.param, user: &user,
			})

			require.Equal(t, tc.code, w.Code, w.Body.String())

			if tc.ec != "" {
				require.Equal(t, tc.ec, errorCode(body))
				return
			}

			data, _ := body["data"].(map[string]any)
			require.Equal(t, true, data["is_read"])
			require.Equal(t, user, inbox.userID)
		})
	}
}
