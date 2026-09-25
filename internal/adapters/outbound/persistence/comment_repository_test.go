package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"gorm.io/gorm"
)

func insertPublishedPost(t *testing.T, tx *gorm.DB) uuid.UUID {
	t.Helper()

	published := time.Now().UTC()

	return createPost(t, NewPostRepository(tx), postFixture{
		author: insertUser(t, tx), status: postdomain.StatusPublished, publishedAt: &published, createdAt: published,
	}).UUID
}

func createComment(t *testing.T, repo *CommentRepository, c commentdomain.Comment) commentdomain.Comment {
	t.Helper()

	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}

	if c.Status == "" {
		c.Status = commentdomain.StatusApproved
	}

	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
		c.UpdatedAt = c.CreatedAt
	}

	got, err := repo.Create(t.Context(), c)
	require.NoError(t, err)

	return got
}

func TestCommentRepositoryPersistsContentHTML(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)

	created := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorUUID: &author, Content: "**hi**", ContentHTML: "<p><strong>hi</strong></p>",
	})
	require.Equal(t, "<p><strong>hi</strong></p>", created.ContentHTML)

	created.Content, created.ContentHTML = "_edited_", "<p><em>edited</em></p>"
	updated, err := repo.Update(t.Context(), created)
	require.NoError(t, err)
	require.Equal(t, "<p><em>edited</em></p>", updated.ContentHTML)

	list, err := repo.List(t.Context(), commentdomain.ListFilter{
		PostUUID: &post, Statuses: commentdomain.PublicStatuses, Page: 1, PerPage: 10,
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, "<p><em>edited</em></p>", list.Items[0].ContentHTML)
}

func TestCommentRepositoryPostPolicy(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	posts := NewPostRepository(tx)
	repo := NewCommentRepository(tx)

	postID := insertPublishedPost(t, tx)

	policy, err := repo.PostPolicy(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.PolicyOpen, policy)

	post, err := posts.GetByID(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, postdomain.CommentPolicyOpen, post.CommentPolicy)

	post.CommentPolicy = postdomain.CommentPolicyDisabled
	post.Version++
	_, err = posts.Update(t.Context(), post)
	require.NoError(t, err)

	policy, err = repo.PostPolicy(t.Context(), postID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.PolicyDisabled, policy)

	draft := createPost(t, posts, postFixture{author: insertUser(t, tx), createdAt: time.Now().UTC()})
	_, err = repo.PostPolicy(t.Context(), draft.UUID)
	require.ErrorIs(t, err, commentdomain.ErrPostNotFound)
}

func TestCommentRepositoryHardDeleteScrubClearsContentHTML(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)

	parent := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorUUID: &author, Content: "secret", ContentHTML: "<p>secret</p>",
	})
	createComment(t, repo, commentdomain.Comment{
		PostUUID: post, ParentUUID: &parent.UUID, AuthorUUID: &author, Content: "reply", Depth: 1,
	})

	scrubbed, err := repo.HardDelete(t.Context(), parent.UUID, commentdomain.ModerationEntry{
		UUID: uuid.New(), CommentUUID: parent.UUID, Action: commentdomain.ActionHardDelete,
		FromStatus: commentdomain.StatusApproved, ToStatus: commentdomain.StatusDeleted, CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.True(t, scrubbed)

	got, err := repo.GetByID(t.Context(), parent.UUID)
	require.NoError(t, err)
	require.Empty(t, got.Content)
	require.Empty(t, got.ContentHTML)
}

func TestCommentRepositoryFlagDedupe(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)
	reporter := insertUser(t, tx)

	comment := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorUUID: &author, Content: "c"})

	flag := func(f commentdomain.Flag) bool {
		t.Helper()

		f.CommentUUID, f.Reason, f.CreatedAt = comment.UUID, "spam", time.Now().UTC()
		added, err := repo.AddFlag(t.Context(), f, 3)
		require.NoError(t, err)

		return added
	}

	require.True(t, flag(commentdomain.Flag{ReporterUUID: &reporter}))
	require.False(t, flag(commentdomain.Flag{ReporterUUID: &reporter}), "same user flags once")
	require.True(t, flag(commentdomain.Flag{ReporterIPHash: "guest-a"}))
	require.False(t, flag(commentdomain.Flag{ReporterIPHash: "guest-a"}), "same guest identity flags once")

	got, err := repo.GetByID(t.Context(), comment.UUID)
	require.NoError(t, err)
	require.Equal(t, 2, got.FlagCount)
	require.Equal(t, commentdomain.StatusApproved, got.Status, "below threshold")

	require.True(t, flag(commentdomain.Flag{ReporterIPHash: "guest-b"}))

	got, err = repo.GetByID(t.Context(), comment.UUID)
	require.NoError(t, err)
	require.Equal(t, 3, got.FlagCount)
	require.Equal(t, commentdomain.StatusFlagged, got.Status, "threshold reached")

	flags, err := repo.ListFlags(t.Context(), comment.UUID)
	require.NoError(t, err)
	require.Len(t, flags, 3)

	_, err = repo.AddFlag(t.Context(), commentdomain.Flag{
		CommentUUID: uuid.New(), ReporterUUID: &reporter, Reason: "spam", CreatedAt: time.Now().UTC(),
	}, 3)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}

func TestCommentRepositoryFlagThresholdLeavesPendingAlone(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	reporter := insertUser(t, tx)

	comment := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorName: "guest", Content: "c", Status: commentdomain.StatusPending,
	})

	added, err := repo.AddFlag(t.Context(), commentdomain.Flag{
		CommentUUID: comment.UUID, ReporterUUID: &reporter, Reason: "abuse", CreatedAt: time.Now().UTC(),
	}, 1)
	require.NoError(t, err)
	require.True(t, added)

	got, err := repo.GetByID(t.Context(), comment.UUID)
	require.NoError(t, err)
	require.Equal(t, 1, got.FlagCount)
	require.Equal(t, commentdomain.StatusPending, got.Status)
}

func TestCommentRepositoryUpvoteToggle(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)
	alice, bob := insertUser(t, tx), insertUser(t, tx)

	comment := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorUUID: &author, Content: "c"})

	toggle := func(voter uuid.UUID) (bool, int) {
		t.Helper()

		upvoted, count, err := repo.ToggleUpvote(t.Context(), comment.UUID, voter, time.Now().UTC())
		require.NoError(t, err)

		return upvoted, count
	}

	upvoted, count := toggle(alice)
	require.True(t, upvoted)
	require.Equal(t, 1, count)

	upvoted, count = toggle(bob)
	require.True(t, upvoted)
	require.Equal(t, 2, count)

	upvoted, count = toggle(alice)
	require.False(t, upvoted)
	require.Equal(t, 1, count)

	upvoted, count = toggle(alice)
	require.True(t, upvoted)
	require.Equal(t, 2, count)

	got, err := repo.GetByID(t.Context(), comment.UUID)
	require.NoError(t, err)
	require.Equal(t, 2, got.UpvoteCount)

	_, _, err = repo.ToggleUpvote(t.Context(), uuid.New(), alice, time.Now().UTC())
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}

func TestCommentRepositoryReplyCountsOnlyPublicReplies(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author := insertUser(t, tx)

	parent := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorUUID: &author, Content: "root"})

	for _, status := range []commentdomain.Status{
		commentdomain.StatusApproved, commentdomain.StatusApproved, commentdomain.StatusDeleted,
		commentdomain.StatusPending, commentdomain.StatusFlagged, commentdomain.StatusSpam, commentdomain.StatusRejected,
	} {
		createComment(t, repo, commentdomain.Comment{
			PostUUID: post, ParentUUID: &parent.UUID, AuthorUUID: &author, Content: "r", Depth: 1, Status: status,
		})
	}

	got, err := repo.GetByID(t.Context(), parent.UUID)
	require.NoError(t, err)
	require.Equal(t, 3, got.ReplyCount, "approved and deleted placeholders only")

	roots, err := repo.List(t.Context(), commentdomain.ListFilter{
		PostUUID: &post, RootsOnly: true, Statuses: commentdomain.PublicStatuses, Page: 1, PerPage: 10,
	})
	require.NoError(t, err)
	require.Len(t, roots.Items, 1)
	require.Equal(t, 3, roots.Items[0].ReplyCount)
}

func TestCommentRepositoryApplyModerationsRollsBackStaleBatch(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	moderator := insertUser(t, tx)

	fresh := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorName: "a", Content: "a", Status: commentdomain.StatusPending,
	})
	stale := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorName: "b", Content: "b", Status: commentdomain.StatusPending,
	})

	approve := func(c commentdomain.Comment, from commentdomain.Status) commentdomain.Moderation {
		now := time.Now().UTC()
		c.Status, c.ModeratedByUUID, c.ModeratedAt, c.UpdatedAt = commentdomain.StatusApproved, &moderator, &now, now

		return commentdomain.Moderation{Comment: c, From: from, Entry: commentdomain.ModerationEntry{
			UUID: uuid.New(), CommentUUID: c.UUID, ModeratorUUID: &moderator, Action: commentdomain.ActionApprove,
			FromStatus: from, ToStatus: commentdomain.StatusApproved, CreatedAt: now,
		}}
	}

	err := repo.ApplyModerations(t.Context(), []commentdomain.Moderation{
		approve(fresh, commentdomain.StatusPending),
		approve(stale, commentdomain.StatusFlagged),
	})

	var batch *commentdomain.BatchError
	require.ErrorAs(t, err, &batch)
	require.ErrorIs(t, err, commentdomain.ErrInvalidTransition)
	require.Equal(t, []uuid.UUID{stale.UUID}, batch.IDs)

	for _, id := range []uuid.UUID{fresh.UUID, stale.UUID} {
		got, err := repo.GetByID(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, commentdomain.StatusPending, got.Status, "whole batch rolled back")
		require.Nil(t, got.ModeratedByUUID)

		history, err := repo.ListModerationLog(t.Context(), id)
		require.NoError(t, err)
		require.Empty(t, history)
	}

	require.NoError(t, repo.ApplyModerations(t.Context(), []commentdomain.Moderation{
		approve(fresh, commentdomain.StatusPending),
	}))

	got, err := repo.GetByID(t.Context(), fresh.UUID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusApproved, got.Status)

	history, err := repo.ListModerationLog(t.Context(), fresh.UUID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, commentdomain.ActionApprove, history[0].Action)
}

func hardDeleteComment(t *testing.T, repo *CommentRepository, id, moderator uuid.UUID) bool {
	t.Helper()

	scrubbed, err := repo.HardDelete(t.Context(), id, commentdomain.ModerationEntry{
		UUID: uuid.New(), CommentUUID: id, ModeratorUUID: &moderator, Action: commentdomain.ActionHardDelete,
		FromStatus: commentdomain.StatusApproved, ToStatus: commentdomain.StatusDeleted,
		Reason: "doxx", CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	return scrubbed
}

func TestCommentRepositoryHardDeleteRemovesLeafAndKeepsLog(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author, moderator := insertUser(t, tx), insertUser(t, tx)

	leaf := createComment(t, repo, commentdomain.Comment{PostUUID: post, AuthorUUID: &author, Content: "leaf"})

	require.False(t, hardDeleteComment(t, repo, leaf.UUID, moderator))

	_, err := repo.GetByID(t.Context(), leaf.UUID)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)

	history, err := repo.ListModerationLog(t.Context(), leaf.UUID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, commentdomain.ActionHardDelete, history[0].Action)
}

func TestCommentRepositoryHardDeleteScrubsParentAndKeepsReplies(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCommentRepository(tx)
	post := insertPublishedPost(t, tx)
	author, voter, moderator := insertUser(t, tx), insertUser(t, tx), insertUser(t, tx)

	parent := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, AuthorUUID: &author, Content: "secret", IPHash: "ip", UserAgent: "ua",
	})
	reply := createComment(t, repo, commentdomain.Comment{
		PostUUID: post, ParentUUID: &parent.UUID, AuthorUUID: &author, Content: "reply", Depth: 1,
	})

	_, _, err := repo.ToggleUpvote(t.Context(), parent.UUID, voter, time.Now().UTC())
	require.NoError(t, err)
	_, err = repo.AddFlag(t.Context(), commentdomain.Flag{
		CommentUUID: parent.UUID, ReporterUUID: &voter, Reason: "doxx", CreatedAt: time.Now().UTC(),
	}, 5)
	require.NoError(t, err)

	require.True(t, hardDeleteComment(t, repo, parent.UUID, moderator))

	got, err := repo.GetByID(t.Context(), parent.UUID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusDeleted, got.Status)
	require.Nil(t, got.AuthorUUID)
	require.Empty(t, got.IPHash)
	require.Empty(t, got.UserAgent)
	require.Zero(t, got.UpvoteCount)
	require.Zero(t, got.FlagCount)
	require.NotNil(t, got.DeletedAt)
	require.Equal(t, &moderator, got.DeletedByUUID)
	require.Equal(t, 1, got.ReplyCount)

	flags, err := repo.ListFlags(t.Context(), parent.UUID)
	require.NoError(t, err)
	require.Empty(t, flags)

	_, err = repo.GetByID(t.Context(), reply.UUID)
	require.NoError(t, err)

	_, err = repo.HardDelete(t.Context(), uuid.New(), commentdomain.ModerationEntry{
		UUID: uuid.New(), Action: commentdomain.ActionHardDelete, CreatedAt: time.Now().UTC(),
	})
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}
