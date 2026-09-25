package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

func TestPolicyDisabledHidesAndClosesComments(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{GuestEnabled: true})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Content: "hi"})
	f.repo.policies[f.post] = commentdomain.PolicyDisabled
	ctx := context.Background()

	_, err := f.svc.ListForPost(ctx, f.post, nil, 1, 20)
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	_, err = f.svc.GetThread(ctx, c.UUID)
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	_, err = f.svc.Create(ctx, CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "new"})
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	_, err = f.svc.Update(ctx, f.user, c.UUID, "edit")
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	_, _, err = f.svc.ToggleUpvote(ctx, f.user, c.UUID)
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	err = f.svc.Flag(ctx, FlagInput{CommentUUID: c.UUID, ReporterUUID: &f.user, Reason: "spam"})
	require.ErrorIs(t, err, commentdomain.ErrCommentsDisabled)

	require.NoError(t, f.svc.Delete(ctx, f.user, c.UUID), "authors can still remove their comments")
}

func TestPolicyReadOnlyShowsButBlocksWrites(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Content: "hi"})
	f.repo.policies[f.post] = commentdomain.PolicyReadOnly
	ctx := context.Background()

	_, err := f.svc.ListForPost(ctx, f.post, nil, 1, 20)
	require.NoError(t, err)

	_, err = f.svc.GetThread(ctx, c.UUID)
	require.NoError(t, err)

	_, err = f.svc.Create(ctx, CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "new"})
	require.ErrorIs(t, err, commentdomain.ErrCommentsClosed)

	_, err = f.svc.Update(ctx, f.user, c.UUID, "edit")
	require.ErrorIs(t, err, commentdomain.ErrCommentsClosed)

	_, _, err = f.svc.ToggleUpvote(ctx, f.user, c.UUID)
	require.ErrorIs(t, err, commentdomain.ErrCommentsClosed)

	require.NoError(t, f.svc.Flag(ctx, FlagInput{CommentUUID: c.UUID, ReporterUUID: &f.user, Reason: "spam"}),
		"readers can still report abuse")
}

func TestPolicyAuthenticatedRejectsGuests(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{GuestEnabled: true})
	f.repo.policies[f.post] = commentdomain.PolicyAuthenticated
	ctx := context.Background()

	_, err := f.svc.Create(ctx, CreateInput{
		PostUUID: f.post, AuthorName: "Ann", AuthorEmail: "ann@example.com", Content: "hi",
	})
	require.ErrorIs(t, err, commentdomain.ErrGuestDisabled)

	_, err = f.svc.Create(ctx, CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"})
	require.NoError(t, err)
}
