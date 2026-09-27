package rbac

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func bufferLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}

	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func TestPolicySyncNotifyWithoutRedisIsNoop(t *testing.T) {
	t.Parallel()

	s := NewPolicySync(nil, nil, 0, slog.New(slog.DiscardHandler))
	s.Notify(context.Background())
	s.Start(context.Background())
	s.Stop()
}

func TestPolicySyncNotifyLogsPublishFailure(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, client.Close())

	logger, logs := bufferLogger()
	NewPolicySync(nil, client, 0, logger).Notify(context.Background())

	assert.Contains(t, logs.String(), `level=WARN msg="rbac policy announcement failed" error="redis: client is closed"`)
}

func TestPolicySyncPublishesInstanceAndIgnoresOwnMessages(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	logger := slog.New(slog.DiscardHandler)
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

func TestPolicySyncReloadsOnPeerAnnouncement(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	adapter := &memAdapter{loaded: make(chan struct{}, 8)}
	enforcer := newMemEnforcer(t, adapter)
	<-adapter.loaded

	logger := slog.New(slog.DiscardHandler)
	self := NewPolicySync(enforcer, client, 0, logger)
	peer := NewPolicySync(nil, client, 0, logger)

	self.Start(context.Background())
	t.Cleanup(self.Stop)

	require.Eventually(t, func() bool { return mr.PubSubNumSub(PolicyChannel)[PolicyChannel] == 1 },
		2*time.Second, 5*time.Millisecond)

	self.Notify(context.Background())
	peer.Notify(context.Background())

	select {
	case <-adapter.loaded:
	case <-time.After(2 * time.Second):
		t.Fatal("peer announcement did not reload the policy")
	}

	self.Stop()
	assert.Equal(t, 2, adapter.loadCount(), "initial load plus the peer announcement; own message ignored")
}

func TestPolicySyncIntervalReload(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		adapter := &memAdapter{}
		enforcer := newMemEnforcer(t, adapter)
		logger, logs := bufferLogger()

		s := NewPolicySync(enforcer, nil, time.Minute, logger)
		s.Start(context.Background())

		synctest.Wait()
		require.Equal(t, 1, adapter.loadCount(), "no reload before the first tick")

		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, adapter.loadCount())
		require.Empty(t, logs.String())

		adapter.setLoadErr(errors.New("db down"))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 3, adapter.loadCount())
		assert.Contains(t, logs.String(), `level=WARN msg="rbac policy reload failed" trigger=interval error="db down"`)

		s.Stop()
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, 3, adapter.loadCount(), "no reloads after Stop")
	})
}

func TestPolicySyncStopWithoutStartIsNoop(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, NewPolicySync(nil, nil, time.Minute, slog.New(slog.DiscardHandler)).Stop)
}

func TestPolicySyncStopReturnsPromptly(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	s := NewPolicySync(nil, client, time.Hour, slog.New(slog.DiscardHandler))
	s.Start(context.Background())

	done := make(chan struct{})

	go func() { s.Stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
}
