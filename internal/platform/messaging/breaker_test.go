package messaging

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
)

func countingHandler(calls *atomic.Int32, errs func(call int32) error) message.HandlerFunc {
	return func(*message.Message) ([]*message.Message, error) {
		return nil, errs(calls.Add(1))
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	transient := errors.New("smtp unavailable")
	handler := CircuitBreaker("email", BreakerConfig{Failures: 2, OpenFor: 20 * time.Millisecond},
		slog.New(slog.DiscardHandler))(countingHandler(&calls, func(call int32) error {
		if call <= 2 {
			return transient
		}

		return nil
	}))

	msg := message.NewMessage(watermill.NewUUID(), nil)

	_, err := handler(msg)
	require.ErrorIs(t, err, transient)
	_, err = handler(msg)
	require.ErrorIs(t, err, transient)

	_, err = handler(msg)
	require.ErrorIs(t, err, ErrCircuitOpen)
	require.Equal(t, int32(2), calls.Load(), "an open breaker does not run the handler")

	require.Eventually(t, func() bool {
		_, err := handler(msg)

		return err == nil
	}, time.Second, 5*time.Millisecond)
	require.Equal(t, int32(3), calls.Load())
}

func TestCircuitBreakerIgnoresPermanentErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	handler := CircuitBreaker("email", BreakerConfig{Failures: 1, OpenFor: time.Minute},
		slog.New(slog.DiscardHandler))(countingHandler(&calls, func(int32) error {
		return fmt.Errorf("bad payload: %w", ErrPermanent)
	}))

	for range 3 {
		_, err := handler(message.NewMessage(watermill.NewUUID(), nil))
		require.ErrorIs(t, err, ErrPermanent)
	}

	require.Equal(t, int32(3), calls.Load())
}

func TestRouterRedeliversCircuitOpenWithoutDeadLettering(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	bus, dead := runRouter(t, func(*message.Message) error {
		if calls.Add(1) == 1 {
			return ErrCircuitOpen
		}

		return nil
	})

	publish(t, bus, message.NewMessage(watermill.NewUUID(), nil))

	require.Eventually(t, func() bool { return calls.Load() == 2 }, 5*time.Second, 5*time.Millisecond)

	select {
	case msg := <-dead:
		t.Fatalf("circuit-open message was dead-lettered: %s", msg.UUID)
	case <-time.After(50 * time.Millisecond):
	}
}
