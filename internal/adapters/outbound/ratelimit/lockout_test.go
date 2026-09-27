package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newTestLockout(t *testing.T, maxFailures int) (*LoginLockout, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	return NewLoginLockout(client, maxFailures, 10*time.Minute), server
}

func TestNewLoginLockoutDefaultsLockPeriod(t *testing.T) {
	t.Parallel()

	for _, lockFor := range []time.Duration{0, -time.Second} {
		server := miniredis.RunT(t)
		client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close() })

		lockedFor, err := NewLoginLockout(client, 1, lockFor).Fail(t.Context(), "k")
		require.NoError(t, err)
		require.Equal(t, 15*time.Minute, lockedFor)
	}
}

func TestLoginLockoutLocksAtThreshold(t *testing.T) {
	t.Parallel()

	lockout, _ := newTestLockout(t, 3)
	ctx := t.Context()

	for range 2 {
		lockedFor, err := lockout.Fail(ctx, "email:a@example.com")
		require.NoError(t, err)
		require.Zero(t, lockedFor)
	}

	remaining, err := lockout.Locked(ctx, "email:a@example.com")
	require.NoError(t, err)
	require.Zero(t, remaining)

	lockedFor, err := lockout.Fail(ctx, "email:a@example.com")
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, lockedFor)

	remaining, err = lockout.Locked(ctx, "email:a@example.com")
	require.NoError(t, err)
	require.InDelta(t, float64(10*time.Minute), float64(remaining), float64(time.Second))

	remaining, err = lockout.Locked(ctx, "email:b@example.com")
	require.NoError(t, err)
	require.Zero(t, remaining)
}

func TestLoginLockoutExpires(t *testing.T) {
	t.Parallel()

	lockout, server := newTestLockout(t, 1)
	ctx := t.Context()

	_, err := lockout.Fail(ctx, "k")
	require.NoError(t, err)

	server.FastForward(11 * time.Minute)

	remaining, err := lockout.Locked(ctx, "k")
	require.NoError(t, err)
	require.Zero(t, remaining)
}

func TestLoginLockoutResetClearsFailures(t *testing.T) {
	t.Parallel()

	lockout, _ := newTestLockout(t, 2)
	ctx := t.Context()

	_, err := lockout.Fail(ctx, "k")
	require.NoError(t, err)
	require.NoError(t, lockout.Reset(ctx, "k"))

	lockedFor, err := lockout.Fail(ctx, "k")
	require.NoError(t, err)
	require.Zero(t, lockedFor)
}

// failSetPipelines fails any pipeline that writes a lock key.
type failSetPipelines struct{}

func (failSetPipelines) DialHook(next goredis.DialHook) goredis.DialHook          { return next }
func (failSetPipelines) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook { return next }

func (failSetPipelines) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() == "set" {
				return errors.New("write refused")
			}
		}

		return next(ctx, cmds)
	}
}

func TestLoginLockoutRedisFailures(t *testing.T) {
	t.Parallel()

	t.Run("locked", func(t *testing.T) {
		t.Parallel()

		lockout, server := newTestLockout(t, 3)
		server.Close()

		remaining, err := lockout.Locked(t.Context(), "k")
		require.Error(t, err)
		require.Zero(t, remaining)
	})

	t.Run("counting a failure", func(t *testing.T) {
		t.Parallel()

		lockout, server := newTestLockout(t, 3)
		server.Close()

		lockedFor, err := lockout.Fail(t.Context(), "k")
		require.Error(t, err)
		require.Zero(t, lockedFor)
	})

	t.Run("setting the lock", func(t *testing.T) {
		t.Parallel()

		server := miniredis.RunT(t)
		client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close() })
		client.AddHook(failSetPipelines{})

		lockout := NewLoginLockout(client, 1, time.Minute)

		lockedFor, err := lockout.Fail(t.Context(), "k")
		require.Error(t, err)
		require.Zero(t, lockedFor)

		remaining, err := lockout.Locked(t.Context(), "k")
		require.NoError(t, err)
		require.Zero(t, remaining, "no lock was written")
	})
}
