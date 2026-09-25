package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
)

func TestCommentWritesRecordEvents(t *testing.T) {
	t.Parallel()

	events := &eventtest.Recorder{}
	f := newFixture(Config{Events: events.Unit()})
	moderator := uuid.New()

	created, err := f.svc.Create(t.Context(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"})
	require.NoError(t, err)

	pending := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending, Content: "a"})
	_, err = f.svc.Moderate(t.Context(), ModerateInput{
		ModeratorUUID: moderator, CommentUUID: pending.UUID, Action: commentdomain.ActionApprove,
	})
	require.NoError(t, err)

	_, err = f.svc.BulkModerate(t.Context(), BulkModerateInput{
		ModeratorUUID: moderator, CommentUUIDs: []uuid.UUID{created.UUID, pending.UUID}, Action: commentdomain.ActionSpam,
	})
	require.NoError(t, err)

	_, err = f.svc.HardDelete(t.Context(), moderator, pending.UUID, "")
	require.NoError(t, err)

	require.Equal(t, []string{
		event.CommentCreated, event.CommentModerated, event.CommentModerated, event.CommentModerated, event.CommentModerated,
	}, events.Types())

	recorded := events.Events()
	require.Equal(t, &f.user, recorded[0].ActorID)
	require.Equal(t, created.UUID, recorded[0].AggregateID)

	approved, ok := recorded[1].Payload.(commentPayload)
	require.True(t, ok)
	require.Equal(t, &moderator, recorded[1].ActorID)
	require.Equal(t, commentPayload{
		CommentID: pending.UUID, PostID: f.post, Status: "approved", Action: "approve", FromStatus: "pending",
	}, approved)

	hardDeleted, ok := recorded[4].Payload.(commentPayload)
	require.True(t, ok)
	require.Equal(t, "hard_delete", hardDeleted.Action)
	require.Equal(t, "spam", hardDeleted.FromStatus)
}

func TestCommentCreateFailsWhenEventCannotBeRecorded(t *testing.T) {
	t.Parallel()

	failure := errors.New("outbox unavailable")
	f := newFixture(Config{Events: (&eventtest.Recorder{Err: failure}).Unit()})

	_, err := f.svc.Create(t.Context(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"})
	require.ErrorIs(t, err, failure)
}
