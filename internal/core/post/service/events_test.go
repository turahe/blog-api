package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

func TestPostWritesRecordEvents(t *testing.T) {
	t.Parallel()

	author, editor, postID := uuid.New(), uuid.New(), uuid.New()
	events := &eventtest.Recorder{}
	repo := newFakePostRepo()
	svc := New(repo, fixedIDs{next: postID}, fixedClock{now: lifecycleNow}).WithEvents(events.Unit())

	created, _, err := svc.CreateDraft(t.Context(), author, "Hello", "", "", "body", nil, nil)
	require.NoError(t, err)

	title := "Hello again"
	_, _, err = svc.Update(t.Context(), created.UUID, author, false, postdomain.UpdateInput{Title: &title})
	require.NoError(t, err)

	_, err = svc.PublishBy(t.Context(), editor, created.UUID)
	require.NoError(t, err)
	_, err = svc.Archive(t.Context(), created.UUID)
	require.NoError(t, err)
	_, err = svc.Publish(t.Context(), created.UUID)
	require.NoError(t, err)
	_, err = svc.Unpublish(t.Context(), created.UUID)
	require.NoError(t, err)

	require.Equal(t, []string{
		event.PostCreated, event.PostUpdated, event.PostPublished, event.PostArchived, event.PostPublished, event.PostUpdated,
	}, events.Types())

	recorded := events.Events()
	require.Equal(t, &author, recorded[0].ActorID)
	require.Equal(t, &editor, recorded[2].ActorID)
	require.Nil(t, recorded[3].ActorID)

	for _, e := range recorded {
		require.Equal(t, event.AggregatePost, e.AggregateType)
		require.Equal(t, postID, e.AggregateID)
	}

	published, ok := recorded[2].Payload.(postPayload)
	require.True(t, ok)
	require.Equal(t, "published", published.Status)
	require.Equal(t, author, published.AuthorID)
	require.Equal(t, lifecycleNow, *published.PublishedAt)
}

func TestPostWriteFailsWhenEventCannotBeRecorded(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusDraft)
	failure := errors.New("outbox unavailable")
	svc := New(newFakePostRepo(post), nil, fixedClock{now: lifecycleNow}).
		WithEvents((&eventtest.Recorder{Err: failure}).Unit())

	_, err := svc.Publish(t.Context(), post.UUID)
	require.ErrorIs(t, err, failure)
}
