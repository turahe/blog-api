package notificationbus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func sample() notificationdomain.Notification {
	actor := uuid.New()

	return notificationdomain.Notification{
		UUID: uuid.New(), UserUUID: uuid.New(), Type: "comment.reply", Title: "t", Body: "b", Preview: "p",
		Payload: map[string]string{"post_id": "x"}, ActorUUID: &actor, DedupeKey: "secret-ish",
		CreatedAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	n := sample()
	payload, err := Encode(n)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "secret-ish", "dedupe keys stay server-side")

	got, err := Decode(payload)
	require.NoError(t, err)

	n.DedupeKey = ""
	require.Equal(t, n, got)

	_, err = Decode([]byte(`{"id":"` + uuid.NewString() + `"}`))
	require.Error(t, err, "a message without a recipient is rejected")

	_, err = Decode([]byte("nope"))
	require.Error(t, err)
}

func TestPublishConsume(t *testing.T) {
	t.Parallel()

	pubSub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})

	t.Cleanup(func() { _ = pubSub.Close() })

	got := make(chan notificationdomain.Notification, 2)

	require.NoError(t, Consume(t.Context(), pubSub, Topic, func(n notificationdomain.Notification) { got <- n }, nil))

	require.NoError(t, pubSub.Publish(Topic, message.NewMessage(watermill.NewUUID(), []byte("garbage"))))

	n := sample()
	NewPublisher(pubSub, Topic, nil).Publish(t.Context(), n)

	select {
	case delivered := <-got:
		require.Equal(t, n.UUID, delivered.UUID, "malformed messages are skipped, later ones still arrive")
		require.Equal(t, n.UserUUID, delivered.UserUUID)
	case <-time.After(5 * time.Second):
		t.Fatal("notification was not delivered")
	}
}

type failingPublisher struct{ err error }

func (p failingPublisher) Publish(string, ...*message.Message) error { return p.err }
func (p failingPublisher) Close() error                              { return nil }

func TestPublishLogsFailures(t *testing.T) {
	t.Parallel()

	unencodable := sample()
	unencodable.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		pub  message.Publisher
		n    notificationdomain.Notification
	}{
		{name: "unencodable notification", pub: failingPublisher{}, n: unencodable},
		{name: "broker refuses", pub: failingPublisher{err: errors.New("broker down")}, n: sample()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &bytes.Buffer{}
			NewPublisher(tt.pub, Topic, slog.New(slog.NewTextHandler(logs, nil))).Publish(t.Context(), tt.n)

			require.Contains(t, logs.String(), "live push not published")
		})
	}
}

type fakeSubscriber struct {
	messages chan *message.Message
	err      error
}

func (s fakeSubscriber) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return s.messages, s.err
}

func (s fakeSubscriber) Close() error { return nil }

func TestConsumeReportsSubscribeFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	err := Consume(t.Context(), fakeSubscriber{err: boom}, Topic, func(notificationdomain.Notification) {
		t.Error("deliver must not be called")
	}, nil)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, Topic)
}

func TestConsumeAcksMalformedMessagesWithoutDelivering(t *testing.T) {
	t.Parallel()

	messages := make(chan *message.Message, 1)
	t.Cleanup(func() { close(messages) })

	logs := &bytes.Buffer{}
	delivered := make(chan notificationdomain.Notification, 1)

	require.NoError(t, Consume(t.Context(), fakeSubscriber{messages: messages}, Topic,
		func(n notificationdomain.Notification) { delivered <- n }, slog.New(slog.NewTextHandler(logs, nil))))

	msg := message.NewMessage(watermill.NewUUID(), []byte("garbage"))
	messages <- msg

	select {
	case <-msg.Acked():
	case <-time.After(5 * time.Second):
		t.Fatal("malformed message was not acknowledged")
	}

	require.Contains(t, logs.String(), "dropped malformed live push")
	require.Empty(t, delivered)
}
