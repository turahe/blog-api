package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis, *goredis.Client) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	return NewRedis(client), server, client
}

// ttlOverride rewrites every PTTL reply in a pipeline, standing in for Redis
// reporting a counter without a positive TTL.
type ttlOverride struct{ ttl time.Duration }

func (ttlOverride) DialHook(next goredis.DialHook) goredis.DialHook          { return next }
func (ttlOverride) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook { return next }

func (h ttlOverride) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)

		for _, cmd := range cmds {
			if ttl, ok := cmd.(*goredis.DurationCmd); ok {
				ttl.SetVal(h.ttl)
			}
		}

		return err
	}
}

func TestRedisAllow(t *testing.T) {
	t.Parallel()

	t.Run("allows up to the limit then rejects with the window remainder", func(t *testing.T) {
		t.Parallel()

		limiter, server, _ := newTestRedis(t)
		ctx := t.Context()

		for range 3 {
			ok, retryAfter, err := limiter.Allow(ctx, "ip:1", 3, time.Minute)
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Zero(t, retryAfter)
		}

		ok, retryAfter, err := limiter.Allow(ctx, "ip:1", 3, time.Minute)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.InDelta(t, float64(time.Minute), float64(retryAfter), float64(time.Second))

		assert.Equal(t, "4", mustGet(t, server, "ratelimit:ip:1"), "counters live under the ratelimit: prefix")
	})

	t.Run("keys are counted independently", func(t *testing.T) {
		t.Parallel()

		limiter, _, _ := newTestRedis(t)
		ctx := t.Context()

		ok, _, err := limiter.Allow(ctx, "a", 1, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)

		ok, _, err = limiter.Allow(ctx, "b", 1, time.Minute)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("window does not slide on later hits", func(t *testing.T) {
		t.Parallel()

		limiter, server, _ := newTestRedis(t)
		ctx := t.Context()

		_, _, err := limiter.Allow(ctx, "k", 1, time.Minute)
		require.NoError(t, err)

		server.FastForward(40 * time.Second)

		ok, retryAfter, err := limiter.Allow(ctx, "k", 1, time.Minute)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.InDelta(t, float64(20*time.Second), float64(retryAfter), float64(time.Second))
	})

	t.Run("a new window resets the count", func(t *testing.T) {
		t.Parallel()

		limiter, server, _ := newTestRedis(t)
		ctx := t.Context()

		for range 2 {
			_, _, err := limiter.Allow(ctx, "k", 1, time.Minute)
			require.NoError(t, err)
		}

		server.FastForward(time.Minute + time.Second)

		ok, retryAfter, err := limiter.Allow(ctx, "k", 1, time.Minute)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Zero(t, retryAfter)
	})

	t.Run("retry after falls back to the window without a ttl", func(t *testing.T) {
		t.Parallel()

		limiter, _, client := newTestRedis(t)
		client.AddHook(ttlOverride{ttl: -1})

		ok, retryAfter, err := limiter.Allow(t.Context(), "k", 0, time.Minute)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, time.Minute, retryAfter)
	})

	t.Run("redis failure is returned", func(t *testing.T) {
		t.Parallel()

		limiter, server, _ := newTestRedis(t)
		server.Close()

		ok, retryAfter, err := limiter.Allow(t.Context(), "k", 1, time.Minute)
		require.Error(t, err)
		assert.False(t, ok)
		assert.Zero(t, retryAfter)
	})
}

func mustGet(t *testing.T, server *miniredis.Miniredis, key string) string {
	t.Helper()

	value, err := server.Get(key)
	require.NoError(t, err)

	return value
}
