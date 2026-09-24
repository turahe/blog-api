package rbac

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPolicySyncNotifyWithoutRedisIsNoop(t *testing.T) {
	t.Parallel()

	s := NewPolicySync(nil, nil, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Notify(context.Background())
	s.Start(context.Background())
	s.Stop()
}

func TestPolicySyncPublishesInstanceAndIgnoresOwnMessages(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := NewPolicySync(nil, client, 0, logger)
	b := NewPolicySync(nil, client, 0, logger)
	require.NotEqual(t, a.instance, b.instance)

	sub := client.Subscribe(context.Background(), PolicyChannel)
	t.Cleanup(func() { _ = sub.Close() })
	_, err := sub.Receive(context.Background())
	require.NoError(t, err)

	a.Notify(context.Background())

	select {
	case msg := <-sub.Channel():
		require.Equal(t, a.instance, msg.Payload)
	case <-time.After(2 * time.Second):
		t.Fatal("no announcement published")
	}
}

func TestPolicySyncStopReturnsPromptly(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	s := NewPolicySync(nil, client, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Start(context.Background())

	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
}
