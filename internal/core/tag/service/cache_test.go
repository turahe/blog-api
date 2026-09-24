package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

func TestTagListCachedAndWritesInvalidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	golang := tagdomain.Tag{UUID: uuid.New(), Name: "Go", Slug: "go"}
	rust := tagdomain.Tag{UUID: uuid.New(), Name: "Rust", Slug: "rust"}
	repo := newFakeRepo(golang, rust)
	cache := readcachetest.New()
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{}).WithCache(cache)

	_, err := svc.List(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"list"}, cache.Keys(readcache.Tags))

	_, err = svc.Create(ctx, "Zig", "")
	require.NoError(t, err)

	name := "Golang"
	_, err = svc.Update(ctx, golang.UUID, &name, nil)
	require.NoError(t, err)
	require.NoError(t, svc.Merge(ctx, rust.UUID, golang.UUID))
	require.NoError(t, svc.ReplacePostTags(ctx, uuid.New(), []uuid.UUID{golang.UUID}))

	require.Equal(t, 3, cache.Invalidations(readcache.Tags))
	require.Equal(t, 2, cache.Invalidations(readcache.Posts), "merge and re-tagging change the posts tag filter")

	_, err = svc.Update(ctx, uuid.New(), &name, nil)
	require.ErrorIs(t, err, tagdomain.ErrNotFound)
	require.Equal(t, 3, cache.Invalidations(readcache.Tags))
}
