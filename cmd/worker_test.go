package cmd

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/messaging"
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

type failingSubscriber struct{}

func (failingSubscriber) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return nil, errors.New("broker unreachable")
}

func (failingSubscriber) Close() error { return nil }

func TestStartRouterReportsSubscribeFailure(t *testing.T) {
	t.Parallel()

	// No router.Close: it waits CloseTimeout (30s) for handlers that never started.
	router, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
	require.NoError(t, err)

	router.AddConsumerHandler("test", "topic", failingSubscriber{}, func(*message.Message) error { return nil })

	done, err := startRouter(t.Context(), router, true)
	require.ErrorContains(t, err, "router: cannot subscribe topic topic: broker unreachable")
	assert.Nil(t, done)
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

func TestAddConsumer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		concurrency int
		want        []string
	}{
		{name: "single copy keeps the bare name", concurrency: 1, want: []string{"email-dispatch"}},
		{name: "copies are numbered", concurrency: 3, want: []string{"email-dispatch-1", "email-dispatch-2", "email-dispatch-3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The router never runs, so there is nothing to Close.
			router, err := message.NewRouter(message.RouterConfig{}, watermill.NopLogger{})
			require.NoError(t, err)

			bus := &messaging.Bus{Subscriber: gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})}
			cfg := config.Config{ConsumerConcurrency: tt.concurrency, ConsumerBreakerFailures: 5}

			addConsumer(router, bus, cfg, emailConsumer, "topic", func(*message.Message) error { return nil },
				slog.New(slog.DiscardHandler))

			assert.ElementsMatch(t, tt.want, slices.Collect(maps.Keys(router.Handlers())))
		})
	}
}
