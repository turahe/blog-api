package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func TestNotification(t *testing.T) {
	t.Parallel()

	actor := uuid.MustParse("0198a1b2-0000-7000-8000-0000000f0002")
	unread := notificationdomain.Notification{
		ID:        9,
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-0000000f0001"),
		Type:      "comment.reply",
		Title:     "New reply",
		Body:      "gopher replied to your comment",
		Preview:   "gopher replied",
		Payload:   map[string]string{"post_id": "p1", "comment_id": "c1", "url": "/posts/a#c1"},
		ActorUUID: &actor,
		DedupeKey: "reply:c1",
		CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}

	read := unread
	read.Payload, read.ActorUUID = nil, nil
	read.ReadAt = new(time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC))

	const common = `"id": "0198a1b2-0000-7000-8000-0000000f0001", "type": "comment.reply", "title": "New reply",
		"body": "gopher replied to your comment", "preview": "gopher replied", "createdAt": "2026-08-01T10:00:00Z"`

	tests := []struct {
		name         string
		notification notificationdomain.Notification
		want         string
	}{
		{
			name:         "unread with links",
			notification: unread,
			want: `{` + common + `, "data": {"postId": "p1", "commentId": "c1", "url": "/posts/a#c1"},
				"actorId": "0198a1b2-0000-7000-8000-0000000f0002", "isRead": false, "readAt": null}`,
		},
		{
			name:         "read system notice",
			notification: read,
			want:         `{` + common + `, "data": {}, "actorId": null, "isRead": true, "readAt": "2026-08-01T11:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Notification(tt.notification))
		})
	}
}
