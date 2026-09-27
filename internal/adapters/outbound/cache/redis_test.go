package cache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
)

type item struct {
	Name string
	At   time.Time
}

func newTestCache(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr(), MaxRetries: -1})

	t.Cleanup(func() { _ = client.Close() })

	return NewRedis(client, map[readcache.Family]time.Duration{
		readcache.Posts: time.Minute,
		readcache.Tags:  time.Hour,
	}, nil), server
}

func load(calls *int, name string) func() (item, error) {
	return func() (item, error) {
		*calls++
		return item{Name: name, At: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)}, nil
	}
}

func TestRedisReadThroughHitsAfterFirstLoad(t *testing.T) {
	t.Parallel()

	cache, server := newTestCache(t)
	ctx := context.Background()
	calls := 0

	for range 3 {
		got, err := readcache.Through(ctx, cache, readcache.Posts, "get:slug=a", load(&calls, "a"))
		require.NoError(t, err)
		require.Equal(t, "a", got.Name)
		require.True(t, got.At.Equal(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)))
	}

	require.Equal(t, 1, calls)

	var entry string

	for _, key := range server.Keys() {
		if strings.HasSuffix(key, ":get:slug=a") {
			entry = key
		}
	}

	require.True(t, strings.HasPrefix(entry, "cache:public:v1:posts:g"), entry)
	require.Equal(t, time.Minute, server.TTL(entry))
}

func TestRedisInvalidateDropsOnlyThatFamily(t *testing.T) {
	t.Parallel()

	cache, _ := newTestCache(t)
	ctx := context.Background()
	posts, tags := 0, 0

	_, _ = readcache.Through(ctx, cache, readcache.Posts, "list", load(&posts, "p"))
	_, _ = readcache.Through(ctx, cache, readcache.Tags, "list", load(&tags, "t"))

	cache.Invalidate(ctx, readcache.Posts)

	_, _ = readcache.Through(ctx, cache, readcache.Posts, "list", load(&posts, "p"))
	_, _ = readcache.Through(ctx, cache, readcache.Tags, "list", load(&tags, "t"))

	require.Equal(t, 2, posts)
	require.Equal(t, 1, tags)
}

func TestRedisFillFromBeforeInvalidateIsNeverServed(t *testing.T) {
	t.Parallel()

	cache, _ := newTestCache(t)
	ctx := context.Background()

	var stale item

	hit, fill := cache.Get(ctx, readcache.Posts, "get:slug=a", &stale)
	require.False(t, hit)

	cache.Invalidate(ctx, readcache.Posts)
	fill(item{Name: "old"})

	calls := 0
	got, err := readcache.Through(ctx, cache, readcache.Posts, "get:slug=a", load(&calls, "new"))
	require.NoError(t, err)
	require.Equal(t, "new", got.Name)
	require.Equal(t, 1, calls)
}

func TestRedisEntriesExpireAndLostGenerationDoesNotResurrect(t *testing.T) {
	t.Parallel()

	cache, server := newTestCache(t)
	ctx := context.Background()
	calls := 0

	_, _ = readcache.Through(ctx, cache, readcache.Posts, "list", load(&calls, "v1"))

	server.FastForward(2 * time.Minute)

	_, _ = readcache.Through(ctx, cache, readcache.Posts, "list", load(&calls, "v2"))
	require.Equal(t, 2, calls, "entry expired after the posts TTL")

	cache.now = func() time.Time { return time.Now().Add(time.Hour) }

	server.Del("cache:public:v1:posts:gen")

	got, err := readcache.Through(ctx, cache, readcache.Posts, "list", load(&calls, "v3"))
	require.NoError(t, err)
	require.Equal(t, "v3", got.Name, "a reseeded generation must not reuse the old entry")
}

func TestRedisSkipsUnconfiguredFamiliesAndBypass(t *testing.T) {
	t.Parallel()

	cache, server := newTestCache(t)
	ctx := context.Background()
	calls := 0

	for range 2 {
		_, _ = readcache.Through(ctx, cache, readcache.Categories, "list", load(&calls, "c"))
		_, _ = readcache.Through(readcache.WithBypass(ctx), cache, readcache.Posts, "list", load(&calls, "p"))
	}

	require.Equal(t, 4, calls)
	require.Empty(t, server.Keys())
}

func TestRedisFailsOpenWhenRedisIsDown(t *testing.T) {
	t.Parallel()

	cache, server := newTestCache(t)
	server.Close()

	calls := 0
	got, err := readcache.Through(context.Background(), cache, readcache.Posts, "list", load(&calls, "db"))
	require.NoError(t, err)
	require.Equal(t, "db", got.Name)

	cache.Invalidate(context.Background(), readcache.Posts)
	require.Error(t, cache.Probe(context.Background()))
}

func TestRedisDoesNotCacheLoadErrors(t *testing.T) {
	t.Parallel()

	cache, server := newTestCache(t)
	boom := errors.New("boom")

	_, err := readcache.Through(context.Background(), cache, readcache.Posts, "get:slug=x", func() (item, error) {
		return item{}, boom
	})
	require.ErrorIs(t, err, boom)

	for _, key := range server.Keys() {
		require.NotContains(t, key, "slug=x")
	}

	require.NoError(t, cache.Probe(context.Background()))
}

// cmdable overrides selected commands of an embedded client.
type cmdable struct {
	goredis.Cmdable

	get   func(ctx context.Context, key string) *goredis.StringCmd
	setNX func(ctx context.Context, key string, value any, ttl time.Duration) *goredis.BoolCmd
}

func (c cmdable) Get(ctx context.Context, key string) *goredis.StringCmd {
	if c.get != nil {
		return c.get(ctx, key)
	}

	return c.Cmdable.Get(ctx, key)
}

func (c cmdable) SetNX(ctx context.Context, key string, value any, ttl time.Duration) *goredis.BoolCmd {
	if c.setNX != nil {
		return c.setNX(ctx, key, value, ttl)
	}

	return c.Cmdable.SetNX(ctx, key, value, ttl)
}

func newLoggedCache(t *testing.T, wrap func(goredis.Cmdable) goredis.Cmdable) (*Redis, *miniredis.Miniredis, *bytes.Buffer) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })

	var cmd goredis.Cmdable = client
	if wrap != nil {
		cmd = wrap(client)
	}

	logs := &bytes.Buffer{}
	cache := NewRedis(cmd, map[readcache.Family]time.Duration{readcache.Posts: time.Minute}, slog.New(slog.NewTextHandler(logs, nil)))

	return cache, server, logs
}

func entryKeyFor(t *testing.T, server *miniredis.Miniredis, suffix string) string {
	t.Helper()

	for _, key := range server.Keys() {
		if strings.HasSuffix(key, suffix) {
			return key
		}
	}

	require.FailNow(t, "no key with suffix", suffix)

	return ""
}

func TestRedisGetFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		corrupt  func(t *testing.T, server *miniredis.Miniredis, key string)
		wantFill bool
		wantLog  string
	}{
		{
			name: "undecodable entry is refilled",
			corrupt: func(t *testing.T, server *miniredis.Miniredis, key string) {
				t.Helper()
				require.NoError(t, server.Set(key, "{not json"))
			},
			wantFill: true,
			wantLog:  "cache entry decode failed",
		},
		{
			name: "lookup error is not refilled",
			corrupt: func(t *testing.T, server *miniredis.Miniredis, key string) {
				t.Helper()
				server.Del(key)
				_, err := server.Lpush(key, "wrong type")
				require.NoError(t, err)
			},
			wantLog: "cache lookup failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache, server, logs := newLoggedCache(t, nil)
			ctx := t.Context()

			var dst item

			hit, fill := cache.Get(ctx, readcache.Posts, "k", &dst)
			require.False(t, hit)
			require.NotNil(t, fill)
			fill(item{Name: "a"})

			tt.corrupt(t, server, entryKeyFor(t, server, ":k"))

			hit, fill = cache.Get(ctx, readcache.Posts, "k", &dst)
			assert.False(t, hit)
			assert.Equal(t, tt.wantFill, fill != nil)
			assert.Contains(t, logs.String(), tt.wantLog)
		})
	}
}

func TestRedisGetSeedFailureSkipsCache(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	cache, server, logs := newLoggedCache(t, func(c goredis.Cmdable) goredis.Cmdable {
		return cmdable{Cmdable: c, setNX: func(context.Context, string, any, time.Duration) *goredis.BoolCmd {
			return goredis.NewBoolResult(false, boom)
		}}
	})

	var dst item

	hit, fill := cache.Get(t.Context(), readcache.Posts, "k", &dst)
	assert.False(t, hit)
	assert.Nil(t, fill)
	assert.Contains(t, logs.String(), "cache generation lookup failed")
	assert.Empty(t, server.Keys())
}

func TestRedisFillFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   any
		down    bool
		wantLog string
	}{
		{name: "unencodable value", value: func() {}, wantLog: "cache entry encode failed"},
		{name: "redis down", value: item{Name: "a"}, down: true, wantLog: "cache fill failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache, server, logs := newLoggedCache(t, nil)

			var dst item

			_, fill := cache.Get(t.Context(), readcache.Posts, "k", &dst)
			require.NotNil(t, fill)

			if tt.down {
				server.Close()
			}

			fill(tt.value)
			assert.Contains(t, logs.String(), tt.wantLog)

			if !tt.down {
				for _, key := range server.Keys() {
					assert.NotContains(t, key, ":k")
				}
			}
		})
	}
}

func TestRedisInvalidateWithoutFamiliesIsNoop(t *testing.T) {
	t.Parallel()

	cache, server, logs := newLoggedCache(t, nil)
	server.Close()

	cache.Invalidate(t.Context())

	assert.Empty(t, logs.String())
}

func TestRedisProbeReadFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		reply   *goredis.StringCmd
		wantErr string
	}{
		{name: "read error", reply: goredis.NewStringResult("", boom), wantErr: "cache probe read"},
		{name: "wrong value", reply: goredis.NewStringResult("nope", nil), wantErr: `cache probe read back "nope"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache, _, _ := newLoggedCache(t, func(c goredis.Cmdable) goredis.Cmdable {
				return cmdable{Cmdable: c, get: func(context.Context, string) *goredis.StringCmd { return tt.reply }}
			})

			err := cache.Probe(t.Context())
			require.ErrorContains(t, err, tt.wantErr)

			if tt.reply.Err() != nil {
				assert.ErrorIs(t, err, boom)
			}
		})
	}
}
