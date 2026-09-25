package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func TestNotificationRepositoryInsertDedupeListRead(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNotificationRepository(tx)
	ctx := t.Context()
	user, other, actor := insertUser(t, tx), insertUser(t, tx), insertUser(t, tx)
	base := time.Now().UTC().Truncate(time.Second)

	first, created, err := repo.Insert(ctx, notificationdomain.Notification{
		UserUUID: user, Type: "comment.reply", Title: "first", Body: "body", Preview: "preview",
		Payload: map[string]string{"post_id": "p1"}, ActorUUID: &actor, DedupeKey: "comment.reply:1", CreatedAt: base,
	})
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, uuid.Nil, first.UUID)

	_, created, err = repo.Insert(ctx, notificationdomain.Notification{
		UserUUID: user, Type: "comment.reply", Title: "dup", DedupeKey: "comment.reply:1", CreatedAt: base,
	})
	require.NoError(t, err)
	require.False(t, created, "same user and dedupe key is a no-op")

	_, created, err = repo.Insert(ctx, notificationdomain.Notification{
		UserUUID: other, Type: "comment.reply", Title: "other", DedupeKey: "comment.reply:1", CreatedAt: base,
	})
	require.NoError(t, err)
	require.True(t, created, "dedupe keys are per user")

	second, created, err := repo.Insert(ctx, notificationdomain.Notification{
		UserUUID: user, Type: "comment.moderated", Title: "second", CreatedAt: base.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, created, "an empty dedupe key never conflicts")

	_, created, err = repo.Insert(ctx, notificationdomain.Notification{UserUUID: uuid.New(), Type: "x", Title: "x", CreatedAt: base})
	require.NoError(t, err)
	require.False(t, created, "unknown recipients are skipped")

	page, err := repo.List(ctx, notificationdomain.ListFilter{UserUUID: user, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, int64(2), page.Unread)
	require.Len(t, page.Items, 2)
	require.Equal(t, second.UUID, page.Items[0].UUID, "newest first")
	require.Equal(t, first.UUID, page.Items[1].UUID)
	require.Equal(t, map[string]string{"post_id": "p1"}, page.Items[1].Payload)
	require.Equal(t, &actor, page.Items[1].ActorUUID)
	require.Equal(t, "comment.reply:1", page.Items[1].DedupeKey)
	require.Empty(t, page.Items[0].Payload)

	readAt := base.Add(time.Hour)

	_, err = repo.MarkRead(ctx, other, first.UUID, readAt)
	require.ErrorIs(t, err, notificationdomain.ErrNotFound, "another user's notification")

	got, err := repo.MarkRead(ctx, user, first.UUID, readAt)
	require.NoError(t, err)
	require.True(t, readAt.Equal(got.ReadAt.UTC()))

	got, err = repo.MarkRead(ctx, user, first.UUID, readAt.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, readAt.Equal(got.ReadAt.UTC()), "marking again keeps the first read time")

	unread, err := repo.List(ctx, notificationdomain.ListFilter{UserUUID: user, UnreadOnly: true, Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), unread.Total)
	require.Equal(t, int64(1), unread.Unread)
	require.Len(t, unread.Items, 1)
	require.Equal(t, second.UUID, unread.Items[0].UUID)

	_, err = repo.MarkRead(ctx, user, uuid.New(), readAt)
	require.ErrorIs(t, err, notificationdomain.ErrNotFound)
}

func TestNotificationRepositoryDirectory(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNotificationRepository(tx)
	comments := NewCommentRepository(tx)
	ctx := t.Context()
	postID := insertPublishedPost(t, tx)
	commenter, pendingOnly := insertUser(t, tx), insertUser(t, tx)

	require.NoError(t, tx.Exec("UPDATE users SET full_name = 'Grace Hopper' WHERE uuid = ?", commenter).Error)
	require.NoError(t, tx.Exec("UPDATE users SET full_name = '  ' WHERE uuid = ?", pendingOnly).Error)

	name, err := repo.DisplayName(ctx, commenter)
	require.NoError(t, err)
	require.Equal(t, "Grace Hopper", name)

	name, err = repo.DisplayName(ctx, pendingOnly)
	require.NoError(t, err)
	require.NotEmpty(t, name, "blank full names fall back to the username")

	_, err = repo.DisplayName(ctx, uuid.New())
	require.ErrorIs(t, err, notificationdomain.ErrNotFound)

	summary, err := repo.PostSummary(ctx, postID)
	require.NoError(t, err)
	require.Equal(t, postID, summary.UUID)
	require.NotEqual(t, uuid.Nil, summary.AuthorUUID)
	require.NotEmpty(t, summary.Slug)

	_, err = repo.PostSummary(ctx, uuid.New())
	require.ErrorIs(t, err, notificationdomain.ErrNotFound)

	createComment(t, comments, commentdomain.Comment{PostUUID: postID, AuthorUUID: &commenter, Content: "a"})
	createComment(t, comments, commentdomain.Comment{PostUUID: postID, AuthorUUID: &commenter, Content: "b"})
	createComment(t, comments, commentdomain.Comment{
		PostUUID: postID, AuthorUUID: &pendingOnly, Content: "c", Status: commentdomain.StatusPending,
	})
	createComment(t, comments, commentdomain.Comment{
		PostUUID: postID, AuthorName: "Guest", AuthorEmail: "guest@example.test", Content: "d",
	})

	commenters, err := repo.PostCommenters(ctx, postID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{commenter}, commenters, "approved registered commenters, once each")
}
