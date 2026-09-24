package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
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
