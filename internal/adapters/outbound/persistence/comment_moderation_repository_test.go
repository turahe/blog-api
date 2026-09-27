package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

func commentIDs(comments []commentdomain.Comment) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(comments))
	for _, c := range comments {
		out = append(out, c.UUID)
	}

	return out
}

func TestCommentRepositoryGetByIDs(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	first := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorName: "a", Content: "a"})
	second := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorName: "b", Content: "b"})

	got, err := repo.GetByIDs(t.Context(), nil)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = repo.GetByIDs(t.Context(), []uuid.UUID{first.UUID, uuid.New(), second.UUID})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{first.UUID, second.UUID}, commentIDs(got), "unknown ids are skipped")

	_, err = repo.GetByIDs(canceledContext(t), []uuid.UUID{first.UUID})
	require.ErrorIs(t, err, context.Canceled)
}

func moderationEntry(c commentdomain.Comment, moderator *uuid.UUID, before, after map[string]any) commentdomain.ModerationEntry {
	return commentdomain.ModerationEntry{
		UUID: uuid.New(), CommentUUID: c.UUID, ModeratorUUID: moderator, Action: commentdomain.ActionApprove,
		FromStatus: commentdomain.StatusPending, ToStatus: commentdomain.StatusApproved, Reason: "looks fine",
		NotifyAuthor: true, Before: before, After: after, CreatedAt: time.Now().UTC(),
	}
}

func approval(c commentdomain.Comment, entry commentdomain.ModerationEntry) commentdomain.Moderation {
	now := time.Now().UTC()
	c.Status, c.ModeratedByUUID, c.ModeratedAt, c.UpdatedAt = commentdomain.StatusApproved, entry.ModeratorUUID, &now, now

	return commentdomain.Moderation{Comment: c, From: commentdomain.StatusPending, Entry: entry}
}

func TestCommentRepositoryApplyModerationsLogsSnapshots(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	ctx := t.Context()
	post := insertPublishedPost(t, tx)
	moderator, departed := insertUser(t, tx), uuid.New()
	pending := func() commentdomain.Comment {
		return createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorName: "g", Content: "c", Status: commentdomain.StatusPending})
	}
	first, second, third := pending(), pending(), pending()

	require.NoError(t, repo.ApplyModerations(ctx, nil))

	before, after := map[string]any{"status": "pending", "flags": float64(2)}, map[string]any{"status": "approved"}
	require.NoError(t, repo.ApplyModerations(ctx, []commentdomain.Moderation{
		approval(first, moderationEntry(first, &moderator, before, after)),
		approval(second, moderationEntry(second, &moderator, nil, nil)),
		approval(third, moderationEntry(third, &departed, nil, nil)),
	}))

	history, err := repo.ListModerationLog(ctx, first.UUID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, &moderator, history[0].ModeratorUUID)
	require.Equal(t, "looks fine", history[0].Reason)
	require.True(t, history[0].NotifyAuthor)
	require.Equal(t, before, history[0].Before)
	require.Equal(t, after, history[0].After)

	history, err = repo.ListModerationLog(ctx, second.UUID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Nil(t, history[0].Before)
	require.Nil(t, history[0].After)

	history, err = repo.ListModerationLog(ctx, third.UUID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Nil(t, history[0].ModeratorUUID, "an unknown moderator is logged without attribution")

	_, err = repo.ListModerationLog(canceledContext(t), first.UUID)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCommentRepositoryModerationRejectsUnencodableSnapshot(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	moderator := insertUser(t, tx)
	unencodable := map[string]any{"ch": make(chan int)}

	tests := []struct {
		name          string
		before, after map[string]any
	}{
		{name: "before", before: unencodable},
		{name: "after", before: map[string]any{"ok": true}, after: unencodable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorName: "g", Content: "c", Status: commentdomain.StatusPending})
			entry := moderationEntry(c, &moderator, tt.before, tt.after)

			err := repo.ApplyModerations(t.Context(), []commentdomain.Moderation{approval(c, entry)})
			require.ErrorContains(t, err, "encode moderation snapshot")

			got, err := repo.GetByID(t.Context(), c.UUID)
			require.NoError(t, err)
			require.Equal(t, commentdomain.StatusPending, got.Status, "the batch rolls back")

			entry.Action = commentdomain.ActionHardDelete
			_, err = repo.HardDelete(t.Context(), c.UUID, entry)
			require.ErrorContains(t, err, "encode moderation snapshot")

			_, err = repo.GetByID(t.Context(), c.UUID)
			require.NoError(t, err, "the comment survives a failed hard delete")
		})
	}
}

func TestCommentRepositoryListModerationLogRejectsNonObjectSnapshot(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)

	tests := []struct {
		name          string
		before, after *string
	}{
		{name: "before", before: new(`[1]`)},
		{name: "after", after: new(`"approved"`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorName: "g", Content: "c"})
			require.NoError(t, tx.Exec(`INSERT INTO comment_moderation_log
				(uuid, comment_id, comment_uuid, action, from_status, to_status, before_state, after_state, created_at)
				VALUES (?, `+idOf("comments")+`, ?, 'approve', 'pending', 'approved', ?, ?, now())`,
				uuid.New(), c.UUID, c.UUID, tt.before, tt.after).Error)

			_, err := repo.ListModerationLog(t.Context(), c.UUID)
			require.ErrorContains(t, err, "decode moderation snapshot")
		})
	}
}

func TestCommentRepositoryStats(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	ctx := t.Context()
	post := insertPublishedPost(t, tx)

	before, err := repo.Stats(ctx)
	require.NoError(t, err)

	// Dated a century back so this post holds the oldest queued comment.
	oldest := time.Now().UTC().AddDate(-100, 0, 0).Truncate(time.Microsecond)

	for i, status := range []commentdomain.Status{
		commentdomain.StatusPending, commentdomain.StatusPending, commentdomain.StatusFlagged, commentdomain.StatusApproved,
	} {
		created := oldest.Add(time.Duration(i) * time.Minute)
		createComment(t, repo, commentdomain.Comment{
			PostUUID: post, AuthorName: "g", Content: "c", Status: status, CreatedAt: created, UpdatedAt: created,
		})
	}

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, before.ByStatus[commentdomain.StatusPending]+2, stats.ByStatus[commentdomain.StatusPending])
	require.Equal(t, before.ByStatus[commentdomain.StatusFlagged]+1, stats.ByStatus[commentdomain.StatusFlagged])
	require.Equal(t, before.ByStatus[commentdomain.StatusApproved]+1, stats.ByStatus[commentdomain.StatusApproved])
	require.Equal(t, before.QueueDepth+3, stats.QueueDepth)
	require.NotNil(t, stats.OldestQueuedAt)
	require.WithinDuration(t, oldest, *stats.OldestQueuedAt, 0)
	require.LessOrEqual(t, len(stats.TopPosts), statsTopPosts)

	var queue *commentdomain.PostQueue

	for i := range stats.TopPosts {
		if stats.TopPosts[i].PostUUID == post {
			queue = &stats.TopPosts[i]
		}
	}

	require.NotNil(t, queue, "the post with queued comments is ranked")
	require.Equal(t, int64(2), queue.Pending)
	require.Equal(t, int64(1), queue.Flagged)
	require.NotEmpty(t, queue.PostTitle)

	_, err = repo.Stats(canceledContext(t))
	require.ErrorIs(t, err, context.Canceled)
}
