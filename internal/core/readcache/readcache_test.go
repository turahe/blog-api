package readcache_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
)

type getCall struct {
	family readcache.Family
	key    string
}

type fakeCache struct {
	hit         bool
	cached      string
	nilFill     bool
	gets        []getCall
	filled      []any
	invalidated [][]readcache.Family
}

func (f *fakeCache) Get(_ context.Context, family readcache.Family, key string, dst any) (bool, func(any)) {
	f.gets = append(f.gets, getCall{family: family, key: key})

	if f.hit {
		*dst.(*string) = f.cached

		return true, nil
	}

	if f.nilFill {
		return false, nil
	}

	return false, func(value any) { f.filled = append(f.filled, value) }
}

func (f *fakeCache) Invalidate(_ context.Context, families ...readcache.Family) {
	f.invalidated = append(f.invalidated, families)
}

func TestWithBypass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	assert.False(t, readcache.Bypassed(ctx))
	assert.True(t, readcache.Bypassed(readcache.WithBypass(ctx)))
}

func TestThrough(t *testing.T) {
	t.Parallel()

	errLoad := errors.New("load failed")

	tests := []struct {
		name       string
		cache      *fakeCache
		nilCache   bool
		bypass     bool
		loadErr    error
		want       string
		wantLoads  int
		wantGets   int
		wantFilled []any
	}{
		{name: "nil cache loads", nilCache: true, want: "loaded", wantLoads: 1},
		{name: "bypass skips cache", cache: &fakeCache{hit: true, cached: "stale"}, bypass: true, want: "loaded", wantLoads: 1},
		{name: "hit returns cached", cache: &fakeCache{hit: true, cached: "cached"}, want: "cached", wantGets: 1},
		{name: "miss loads and fills", cache: &fakeCache{}, want: "loaded", wantLoads: 1, wantGets: 1, wantFilled: []any{"loaded"}},
		{name: "miss with nil fill loads only", cache: &fakeCache{nilFill: true}, want: "loaded", wantLoads: 1, wantGets: 1},
		{name: "load error is not cached", cache: &fakeCache{}, loadErr: errLoad, want: "loaded", wantLoads: 1, wantGets: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tt.bypass {
				ctx = readcache.WithBypass(ctx)
			}

			var cache readcache.Cache
			if !tt.nilCache {
				cache = tt.cache
			}

			loads := 0
			got, err := readcache.Through(ctx, cache, readcache.Posts, "k", func() (string, error) {
				loads++

				return "loaded", tt.loadErr
			})

			require.ErrorIs(t, err, tt.loadErr)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantLoads, loads)

			if tt.cache != nil {
				assert.Len(t, tt.cache.gets, tt.wantGets)
				assert.Equal(t, tt.wantFilled, tt.cache.filled)

				if tt.wantGets > 0 {
					assert.Equal(t, getCall{family: readcache.Posts, key: "k"}, tt.cache.gets[0])
				}
			}
		})
	}
}

func TestInvalidate(t *testing.T) {
	t.Parallel()

	t.Run("nil cache is a no-op", func(t *testing.T) {
		t.Parallel()

		assert.NotPanics(t, func() { readcache.Invalidate(context.Background(), nil, readcache.Posts) })
	})

	t.Run("forwards families", func(t *testing.T) {
		t.Parallel()

		cache := &fakeCache{}
		readcache.Invalidate(context.Background(), cache, readcache.Posts, readcache.Tags)

		assert.Equal(t, [][]readcache.Family{{readcache.Posts, readcache.Tags}}, cache.invalidated)
	})
}

func TestKeyNormalisesQueryParts(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	var none *uuid.UUID

	require.Equal(t, "list", readcache.Key("list"))
	require.Equal(t,
		"list:page=2:per_page=20:category=11111111-1111-1111-1111-111111111111:tag=-",
		readcache.Key("list", "page", 2, "per_page", 20, "category", &id, "tag", none))
	require.Equal(t, "get:slug=-", readcache.Key("get", "slug", ""))
	require.Equal(t, "get:slug=a", readcache.Key("get", "slug", "a", "dangling"))
}
