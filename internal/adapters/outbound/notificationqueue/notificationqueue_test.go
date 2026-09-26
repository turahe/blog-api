package notificationqueue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

// recorder stands in for both the inline inbox and the worker's deliverer.
type recorder struct {
	replies     [][2]commentdomain.Comment
	moderations []commentdomain.Moderation
	posts       []postdomain.Post
	actors      []*uuid.UUID
	err         error
}

func (r *recorder) CommentReplied(ctx context.Context, reply, parent commentdomain.Comment) {
	_ = r.DeliverCommentReplied(ctx, reply, parent)
}

func (r *recorder) CommentModerated(ctx context.Context, change commentdomain.Moderation) {
	_ = r.DeliverCommentModerated(ctx, change)
}

func (r *recorder) PostPublished(ctx context.Context, post postdomain.Post, actorID *uuid.UUID) {
	_ = r.DeliverPostPublished(ctx, post, actorID)
}

func (r *recorder) DeliverCommentReplied(_ context.Context, reply, parent commentdomain.Comment) error {
	r.replies = append(r.replies, [2]commentdomain.Comment{reply, parent})
	return r.err
}

func (r *recorder) DeliverCommentModerated(_ context.Context, change commentdomain.Moderation) error {
	r.moderations = append(r.moderations, change)
	return r.err
}

func (r *recorder) DeliverPostPublished(_ context.Context, post postdomain.Post, actorID *uuid.UUID) error {
	r.posts = append(r.posts, post)
	r.actors = append(r.actors, actorID)

	return r.err
}

// relay hands the recorded command to the worker handler the way the outbox relay does.
func relay(t *testing.T, events *eventtest.Recorder, worker Deliverer) {
	t.Helper()

	recorded := events.Events()
	require.Len(t, recorded, 1)
	require.Equal(t, event.NotificationRequested, recorded[0].Type)

	payload, err := json.Marshal(recorded[0].Payload)
	require.NoError(t, err)
	require.NoError(t, Handler(worker)(message.NewMessage(recorded[0].ID.String(), payload)))
}

func TestCommentRepliedRoundTrip(t *testing.T) {
	t.Parallel()

	author, replier := uuid.New(), uuid.New()
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: uuid.New(), AuthorUUID: &author}
	reply := commentdomain.Comment{
		UUID: uuid.New(), PostUUID: parent.PostUUID, AuthorUUID: &replier, AuthorName: "Grace", Content: "Great point",
		AuthorEmail: "grace@example.com", UserAgent: "curl/8.0",
	}

	events, inline, worker := &eventtest.Recorder{}, &recorder{}, &recorder{}
	New(events, inline, nil).CommentReplied(t.Context(), reply, parent)
	require.Empty(t, inline.replies)

	payload, err := json.Marshal(events.Events()[0].Payload)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "grace@example.com")
	require.NotContains(t, string(payload), "curl/8.0")

	relay(t, events, worker)
	require.Len(t, worker.replies, 1)

	got, gotParent := worker.replies[0][0], worker.replies[0][1]
	require.Equal(t, reply.UUID, got.UUID)
	require.Equal(t, reply.PostUUID, got.PostUUID)
	require.Equal(t, reply.AuthorUUID, got.AuthorUUID)
	require.Equal(t, reply.AuthorName, got.AuthorName)
	require.Equal(t, reply.Content, got.Content)
	require.Equal(t, parent.UUID, gotParent.UUID)
	require.Equal(t, parent.AuthorUUID, gotParent.AuthorUUID)
}

func TestCommentModeratedRoundTrip(t *testing.T) {
	t.Parallel()

	author, moderator := uuid.New(), uuid.New()
	change := commentdomain.Moderation{
		Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: uuid.New(), AuthorUUID: &author},
		Entry: commentdomain.ModerationEntry{
			UUID: uuid.New(), ModeratorUUID: &moderator, ToStatus: commentdomain.StatusSpam, Reason: "link farm",
		},
	}

	events, worker := &eventtest.Recorder{}, &recorder{}
	New(events, &recorder{}, nil).CommentModerated(t.Context(), change)
	relay(t, events, worker)

	require.Len(t, worker.moderations, 1)
	got := worker.moderations[0]
	require.Equal(t, change.Comment.UUID, got.Comment.UUID)
	require.Equal(t, change.Comment.PostUUID, got.Comment.PostUUID)
	require.Equal(t, change.Comment.AuthorUUID, got.Comment.AuthorUUID)
	require.Equal(t, change.Entry.UUID, got.Entry.UUID)
	require.Equal(t, change.Entry.ModeratorUUID, got.Entry.ModeratorUUID)
	require.Equal(t, change.Entry.ToStatus, got.Entry.ToStatus)
	require.Equal(t, change.Entry.Reason, got.Entry.Reason)
}

func TestPostPublishedRoundTrip(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	editor := uuid.New()
	post := postdomain.Post{UUID: uuid.New(), AuthorUUID: uuid.New(), Title: "Hello", Slug: "hello", PublishedAt: &published}

	events, worker := &eventtest.Recorder{}, &recorder{}
	New(events, &recorder{}, nil).PostPublished(t.Context(), post, &editor)
	require.Equal(t, event.AggregatePost, events.Events()[0].AggregateType)
	relay(t, events, worker)

	require.Len(t, worker.posts, 1)
	got := worker.posts[0]
	require.Equal(t, post.UUID, got.UUID)
	require.Equal(t, post.AuthorUUID, got.AuthorUUID)
	require.Equal(t, post.Title, got.Title)
	require.Equal(t, post.Slug, got.Slug)
	require.True(t, published.Equal(*got.PublishedAt))
	require.Equal(t, &editor, worker.actors[0])
}

func TestNotifierDeliversInlineWhenCommandCannotBeStored(t *testing.T) {
	t.Parallel()

	inline := &recorder{}
	post := postdomain.Post{UUID: uuid.New(), AuthorUUID: uuid.New(), Title: "Hello", Slug: "hello"}

	New(&eventtest.Recorder{Err: errors.New("db down")}, inline, slog.New(slog.DiscardHandler)).
		PostPublished(t.Context(), post, nil)
	require.Equal(t, []postdomain.Post{post}, inline.posts)
}

func TestHandlerRejectsMalformedCommandsPermanently(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]string{
		"not json":       `nope`,
		"unknown kind":   `{"kind":"digest"}`,
		"missing parent": `{"kind":"comment_replied","reply":{"id":"` + uuid.NewString() + `"}}`,
		"missing post":   `{"kind":"post_published"}`,
	} {
		err := Handler(&recorder{})(message.NewMessage("m", []byte(payload)))
		require.ErrorIs(t, err, messaging.ErrPermanent, name)
	}
}

func TestHandlerReturnsRetryableDeliveryError(t *testing.T) {
	t.Parallel()

	payload := `{"kind":"post_published","post":{"id":"` + uuid.NewString() + `","author_id":"` + uuid.NewString() + `"}}`
	err := Handler(&recorder{err: errors.New("db down")})(message.NewMessage("m", []byte(payload)))
	require.Error(t, err)
	require.NotErrorIs(t, err, messaging.ErrPermanent)
}
