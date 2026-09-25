package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/stretchr/testify/require"
)

func TestStartRouterReturnsOnceSubscribed(t *testing.T) {
	t.Parallel()

	pubsub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	router, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
	require.NoError(t, err)

	handled := make(chan struct{}, 1)

	router.AddConsumerHandler("test", "topic", pubsub, func(*message.Message) error {
		handled <- struct{}{}
		return nil
	})

	ctx, cancel := context.WithCancel(t.Context())

	done, err := startRouter(ctx, router, true)
	require.NoError(t, err)

	// A non-persistent gochannel only delivers to existing subscribers.
	require.NoError(t, pubsub.Publish("topic", message.NewMessage(watermill.NewUUID(), nil)))

	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("message published after startRouter was not handled")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestStartRouterWithoutConsumersWaitsForShutdown(t *testing.T) {
	t.Parallel()

	router, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	done, err := startRouter(ctx, router, false)
	require.NoError(t, err)

	select {
	case <-done:
		t.Fatal("returned before shutdown")
	default:
	}

	cancel()
	require.NoError(t, <-done)
}
