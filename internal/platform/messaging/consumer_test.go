package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/logging"
)

const testTopic = "test.topic"

// runRouter starts a router over an in-memory bus with handler on testTopic and returns the
// bus and a subscription to the dead-letter topic.
func runRouter(t *testing.T, handler message.NoPublishHandlerFunc) (*Bus, <-chan *message.Message) {
	t.Helper()

	pubsub := gochannel.NewGoChannel(gochannel.Config{Persistent: true}, watermill.NopLogger{})
	bus := &Bus{Publisher: pubsub, Subscriber: pubsub, Logger: watermill.NopLogger{}}

	router, err := NewRouter(bus, ConsumerConfig{MaxRetries: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond})
	require.NoError(t, err)

	router.AddConsumerHandler("test-handler", testTopic, bus.Subscriber, handler)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = router.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	<-router.Running()

	dead, err := pubsub.Subscribe(t.Context(), DeadLetterTopic)
	require.NoError(t, err)

	return bus, dead
}

func publish(t *testing.T, bus *Bus, msg *message.Message) {
	t.Helper()

	require.NoError(t, bus.Publisher.Publish(testTopic, msg))
}

func receive(t *testing.T, ch <-chan *message.Message) *message.Message {
	t.Helper()

	select {
	case msg := <-ch:
		msg.Ack()

		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")

		return nil
	}
}

func TestRouterDeadLettersAfterRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	bus, dead := runRouter(t, func(*message.Message) error {
		calls.Add(1)

		return errors.New("smtp unavailable")
	})

	publish(t, bus, message.NewMessage("m-1", []byte(`{}`)))

	poisoned := receive(t, dead)
	require.Equal(t, "m-1", poisoned.UUID)
	require.Equal(t, "smtp unavailable", poisoned.Metadata.Get(middleware.ReasonForPoisonedKey))
	require.Equal(t, testTopic, poisoned.Metadata.Get(middleware.PoisonedTopicKey))
	require.Equal(t, "test-handler", poisoned.Metadata.Get(middleware.PoisonedHandlerKey))
	require.EqualValues(t, 3, calls.Load(), "first attempt plus MaxRetries")
}

func TestRouterDeadLettersPermanentErrorWithoutRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	bus, dead := runRouter(t, func(*message.Message) error {
		calls.Add(1)

		return fmt.Errorf("bad payload: %w", ErrPermanent)
	})

	publish(t, bus, message.NewMessage("m-2", nil))

	require.Equal(t, "m-2", receive(t, dead).UUID)
	require.EqualValues(t, 1, calls.Load())
}

func TestRouterRecoversPanicAndRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	handled := make(chan string, 1)

	bus, dead := runRouter(t, func(msg *message.Message) error {
		if calls.Add(1) == 1 {
			panic("transient")
		}

		handled <- logging.RequestID(msg.Context())

		return nil
	})

	msg := message.NewMessage("m-3", nil)
	msg.Metadata.Set(CorrelationIDKey, "req-123")
	publish(t, bus, msg)

	select {
	case requestID := <-handled:
		require.Equal(t, "req-123", requestID)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not succeed after retry")
	}

	select {
	case poisoned := <-dead:
		t.Fatalf("unexpected dead letter %s", poisoned.UUID)
	case <-time.After(50 * time.Millisecond):
	}
}

type memoryDeduper struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (d *memoryDeduper) Once(ctx context.Context, consumer, messageID string, fn func(ctx context.Context) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := consumer + "/" + messageID
	if d.seen[key] {
		return nil
	}

	if err := fn(ctx); err != nil {
		return err
	}

	d.seen[key] = true

	return nil
}

func TestIdempotentRunsHandlerOncePerMessage(t *testing.T) {
	t.Parallel()

	var calls int

	failNext := true
	handler := Idempotent(&memoryDeduper{seen: map[string]bool{}}, "email", func(*message.Message) error {
		calls++

		if failNext {
			failNext = false

			return errors.New("transient")
		}

		return nil
	})

	require.Error(t, handler(message.NewMessage("m-1", nil)))
	require.NoError(t, handler(message.NewMessage("m-1", nil)), "a failed attempt does not claim the message")
	require.NoError(t, handler(message.NewMessage("m-1", nil)))
	require.NoError(t, handler(message.NewMessage("m-2", nil)))
	require.Equal(t, 3, calls, "the duplicate m-1 delivery is skipped")

	require.ErrorIs(t, handler(message.NewMessage("", nil)), ErrMissingMessageID)
}
