package notificationbus

import (
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
