package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

func TestTransitionTable(t *testing.T) {
	t.Parallel()

	statuses := []commentdomain.Status{
		commentdomain.StatusPending, commentdomain.StatusApproved, commentdomain.StatusFlagged,
		commentdomain.StatusSpam, commentdomain.StatusRejected, commentdomain.StatusDeleted,
	}
	allowed := map[commentdomain.Action]map[commentdomain.Status]commentdomain.Status{
		commentdomain.ActionApprove: {
			commentdomain.StatusPending:  commentdomain.StatusApproved,
			commentdomain.StatusFlagged:  commentdomain.StatusApproved,
			commentdomain.StatusRejected: commentdomain.StatusApproved,
			commentdomain.StatusSpam:     commentdomain.StatusApproved,
		},
		commentdomain.ActionReject: {
			commentdomain.StatusPending:  commentdomain.StatusRejected,
			commentdomain.StatusFlagged:  commentdomain.StatusRejected,
			commentdomain.StatusApproved: commentdomain.StatusRejected,
			commentdomain.StatusSpam:     commentdomain.StatusRejected,
		},
		commentdomain.ActionSpam: {
			commentdomain.StatusPending:  commentdomain.StatusSpam,
			commentdomain.StatusFlagged:  commentdomain.StatusSpam,
			commentdomain.StatusApproved: commentdomain.StatusSpam,
			commentdomain.StatusRejected: commentdomain.StatusSpam,
		},
		commentdomain.ActionRestore: {
			commentdomain.StatusDeleted: commentdomain.StatusApproved,
		},
	}

	for action, targets := range allowed {
		for _, from := range statuses {
			got, err := commentdomain.Transition(from, action)

			want, ok := targets[from]
			if !ok {
				require.ErrorIs(t, err, commentdomain.ErrInvalidTransition, "%s from %s", action, from)
				continue
			}

			require.NoError(t, err, "%s from %s", action, from)
			require.Equal(t, want, got, "%s from %s", action, from)
		}
	}

	_, err := commentdomain.Transition(commentdomain.StatusPending, commentdomain.ActionHardDelete)
	require.ErrorIs(t, err, commentdomain.ErrValidation)
}

func TestModerateApprovesAndLogs(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "hi"})
	moderator := uuid.New()

	got, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: moderator, CommentUUID: c.UUID, Action: commentdomain.ActionApprove,
		Reason: "  looks fine ", NotifyAuthor: true,
	})

	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusApproved, got.Status)
	require.Equal(t, &moderator, got.ModeratedByUUID)
	require.Equal(t, "looks fine", got.ModerationReason)
	require.Equal(t, f.clock.now, *got.ModeratedAt)

	require.Len(t, f.repo.log, 1)
	entry := f.repo.log[0]
	require.Equal(t, commentdomain.ActionApprove, entry.Action)
	require.Equal(t, commentdomain.StatusPending, entry.FromStatus)
	require.Equal(t, commentdomain.StatusApproved, entry.ToStatus)
	require.True(t, entry.NotifyAuthor)
	require.Equal(t, "pending", entry.Before["status"])
	require.Equal(t, "approved", entry.After["status"])
}

func TestModerateApprovingFlaggedResetsFlagCount(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	flagged := f.seed(commentdomain.Comment{Status: commentdomain.StatusFlagged, FlagCount: 3, Content: "hi"})
	pending := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, FlagCount: 1, Content: "hi"})

	for _, c := range []commentdomain.Comment{flagged, pending} {
		_, err := f.svc.Moderate(context.Background(), ModerateInput{
			ModeratorUUID: uuid.New(), CommentUUID: c.UUID, Action: commentdomain.ActionApprove,
		})
		require.NoError(t, err)
	}

	require.Equal(t, 0, f.repo.comments[flagged.UUID].FlagCount)
	require.Equal(t, 1, f.repo.comments[pending.UUID].FlagCount)
}

func TestModerateRestoreClearsDeletion(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	now := f.clock.now
	c := f.seed(commentdomain.Comment{
		Status: commentdomain.StatusDeleted, Content: "oops", DeletedAt: &now, DeletedByUUID: &f.user,
	})

	got, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: c.UUID, Action: commentdomain.ActionRestore,
	})

	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusApproved, got.Status)
	require.Nil(t, got.DeletedAt)
	require.Nil(t, got.DeletedByUUID)
}

func TestModerateRejectsScrubbedRestore(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusDeleted})

	_, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: c.UUID, Action: commentdomain.ActionRestore,
	})

	require.ErrorIs(t, err, commentdomain.ErrInvalidTransition)
}

func TestModerateValidation(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "hi"})

	cases := map[string]ModerateInput{
		"unknown action":     {CommentUUID: c.UUID, Action: "delete"},
		"hard delete action": {CommentUUID: c.UUID, Action: commentdomain.ActionHardDelete},
		"long reason": {
			CommentUUID: c.UUID, Action: commentdomain.ActionApprove,
			Reason: strings.Repeat("x", commentdomain.MaxModerationReasonRunes+1),
		},
	}
	for name, in := range cases {
		_, err := f.svc.Moderate(context.Background(), in)
		require.ErrorIs(t, err, commentdomain.ErrValidation, name)
	}

	_, err := f.svc.Moderate(context.Background(), ModerateInput{CommentUUID: uuid.New(), Action: commentdomain.ActionApprove})
	require.ErrorIs(t, err, commentdomain.ErrNotFound)

	_, err = f.svc.Moderate(context.Background(), ModerateInput{CommentUUID: c.UUID, Action: commentdomain.ActionRestore})
	require.ErrorIs(t, err, commentdomain.ErrInvalidTransition)
	require.Empty(t, f.repo.log)
}

func TestModerateLosesRaceWithAnotherModerator(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "hi"})
	repo := &racingRepo{fakeRepo: f.repo, concurrent: commentdomain.StatusSpam}
	svc := New(repo, seqIDs{}, f.clock, Config{})

	_, err := svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: c.UUID, Action: commentdomain.ActionApprove,
	})

	require.ErrorIs(t, err, commentdomain.ErrInvalidTransition)
	require.Equal(t, commentdomain.StatusSpam, f.repo.comments[c.UUID].Status)
	require.Empty(t, f.repo.log)
}

// racingRepo changes every comment's status between the service's read and its write.
type racingRepo struct {
	*fakeRepo
	concurrent commentdomain.Status
}

func (r *racingRepo) ApplyModerations(ctx context.Context, changes []commentdomain.Moderation) error {
	for _, change := range changes {
		c := r.comments[change.Comment.UUID]
		c.Status = r.concurrent
		r.comments[c.UUID] = c
	}

	return r.fakeRepo.ApplyModerations(ctx, changes)
}

func TestBulkModerateAppliesAll(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	a := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "a"})
	b := f.seed(commentdomain.Comment{Status: commentdomain.StatusFlagged, Content: "b"})

	n, err := f.svc.BulkModerate(context.Background(), BulkModerateInput{
		ModeratorUUID: uuid.New(), CommentUUIDs: []uuid.UUID{a.UUID, b.UUID, a.UUID},
		Action: commentdomain.ActionSpam, Reason: "link farm",
	})

	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, commentdomain.StatusSpam, f.repo.comments[a.UUID].Status)
	require.Equal(t, commentdomain.StatusSpam, f.repo.comments[b.UUID].Status)
	require.Len(t, f.repo.log, 2)
}

func TestBulkModerateIsAllOrNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	ok := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "a"})
	approved := f.seed(commentdomain.Comment{Status: commentdomain.StatusApproved, Content: "b"})
	missing := uuid.New()

	_, err := f.svc.BulkModerate(context.Background(), BulkModerateInput{
		ModeratorUUID: uuid.New(), CommentUUIDs: []uuid.UUID{ok.UUID, approved.UUID}, Action: commentdomain.ActionApprove,
	})

	var batch *commentdomain.BatchError
	require.ErrorAs(t, err, &batch)
	require.ErrorIs(t, err, commentdomain.ErrInvalidTransition)
	require.Equal(t, []uuid.UUID{approved.UUID}, batch.IDs)

	_, err = f.svc.BulkModerate(context.Background(), BulkModerateInput{
		ModeratorUUID: uuid.New(), CommentUUIDs: []uuid.UUID{ok.UUID, missing}, Action: commentdomain.ActionApprove,
	})
	require.ErrorAs(t, err, &batch)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
	require.Equal(t, []uuid.UUID{missing}, batch.IDs)

	require.Equal(t, commentdomain.StatusPending, f.repo.comments[ok.UUID].Status)
	require.Empty(t, f.repo.log)
}

func TestBulkModerateValidation(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	tooMany := make([]uuid.UUID, commentdomain.MaxBulkModerate+1)

	for i := range tooMany {
		tooMany[i] = uuid.New()
	}

	cases := map[string]BulkModerateInput{
		"empty":          {Action: commentdomain.ActionApprove},
		"too many":       {CommentUUIDs: tooMany, Action: commentdomain.ActionApprove},
		"unknown action": {CommentUUIDs: []uuid.UUID{uuid.New()}, Action: "nuke"},
	}
	for name, in := range cases {
		_, err := f.svc.BulkModerate(context.Background(), in)
		require.ErrorIs(t, err, commentdomain.ErrValidation, name)
	}
}

func TestHardDeleteRemovesLeafComment(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusSpam, Content: "junk"})
	moderator := uuid.New()

	scrubbed, err := f.svc.HardDelete(context.Background(), moderator, c.UUID, "illegal")

	require.NoError(t, err)
	require.False(t, scrubbed)
	require.NotContains(t, f.repo.comments, c.UUID)
	require.Len(t, f.repo.log, 1)
	require.Equal(t, commentdomain.ActionHardDelete, f.repo.log[0].Action)
	require.Equal(t, commentdomain.StatusSpam, f.repo.log[0].FromStatus)
	require.Equal(t, "illegal", f.repo.log[0].Reason)
	require.Equal(t, &moderator, f.repo.log[0].ModeratorUUID)
	require.Equal(t, "junk", f.repo.log[0].Before["content"])
}

func TestHardDeleteScrubsCommentWithReplies(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	parent := f.seed(commentdomain.Comment{Content: "doxx", AuthorName: "Ann", AuthorEmail: "ann@example.com"})
	reply := f.seed(commentdomain.Comment{Content: "reply", ParentUUID: &parent.UUID, Depth: 1})

	scrubbed, err := f.svc.HardDelete(context.Background(), uuid.New(), parent.UUID, "")

	require.NoError(t, err)
	require.True(t, scrubbed)
	require.Contains(t, f.repo.comments, reply.UUID)
	require.True(t, f.repo.comments[parent.UUID].Scrubbed())
	require.Empty(t, f.repo.comments[parent.UUID].AuthorEmail)

	_, err = f.svc.HardDelete(context.Background(), uuid.New(), uuid.New(), "")
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}

func TestAdminListDefaultsToQueue(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, err := f.svc.AdminList(context.Background(), AdminListInput{Page: 0, PerPage: 500})
	require.NoError(t, err)
	require.Equal(t, commentdomain.QueueStatuses, f.repo.lastFilter.Statuses)
	require.Equal(t, 1, f.repo.lastFilter.Page)
	require.Equal(t, 20, f.repo.lastFilter.PerPage)

	_, err = f.svc.AdminList(context.Background(), AdminListInput{
		Statuses: []commentdomain.Status{commentdomain.StatusDeleted}, PostUUID: &f.post, NewestFirst: true,
	})
	require.NoError(t, err)
	require.Equal(t, []commentdomain.Status{commentdomain.StatusDeleted}, f.repo.lastFilter.Statuses)
	require.Equal(t, &f.post, f.repo.lastFilter.PostUUID)
	require.True(t, f.repo.lastFilter.NewestFirst)

	_, err = f.svc.AdminList(context.Background(), AdminListInput{Statuses: []commentdomain.Status{"bogus"}})
	require.ErrorIs(t, err, commentdomain.ErrValidation)
}

func TestAdminGetIncludesFlagsAndHistory(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{Status: commentdomain.StatusFlagged, Content: "hi"})
	f.repo.flags = append(f.repo.flags,
		commentdomain.Flag{CommentUUID: c.UUID, Reason: "spam"},
		commentdomain.Flag{CommentUUID: uuid.New(), Reason: "abuse"},
	)

	_, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: c.UUID, Action: commentdomain.ActionReject,
	})
	require.NoError(t, err)

	review, err := f.svc.AdminGet(context.Background(), c.UUID)
	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusRejected, review.Comment.Status)
	require.Len(t, review.Flags, 1)
	require.Len(t, review.History, 1)

	_, err = f.svc.AdminGet(context.Background(), uuid.New())
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}
