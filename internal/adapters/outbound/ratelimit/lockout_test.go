package ratelimit

import (
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
