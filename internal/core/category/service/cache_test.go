package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
)

func TestCategoryReadsCachedAndWritesInvalidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	news := categorydomain.Category{UUID: uuid.New(), Name: "News", Slug: "news"}
	repo := newFakeRepo(news)
	cache := readcachetest.New()
	svc := New(repo, &fixedIDs{}, fixedClock{}).WithCache(cache)

	_, err := svc.List(ctx)
	require.NoError(t, err)
	_, err = svc.GetBySlug(ctx, "news")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"list", "get:slug=news"}, cache.Keys(readcache.Categories))

	delete(repo.bySlug, "news")

	got, err := svc.GetBySlug(ctx, "news")
	require.NoError(t, err, "served from cache without touching the repository")
	require.Equal(t, news.UUID, got.UUID)

	name := "World"
	created, err := svc.Create(ctx, CreateInput{Name: "Sport"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, news.UUID, UpdateInput{Name: &name})
	require.NoError(t, err)
	_, err = svc.Move(ctx, created.UUID, &news.UUID, nil)
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, created.UUID))
	require.NoError(t, svc.RebuildAll(ctx))

	require.Equal(t, 5, cache.Invalidations(readcache.Categories))
	require.Empty(t, cache.Keys(readcache.Categories))

	_, err = svc.Update(ctx, uuid.New(), UpdateInput{Name: &name})
	require.ErrorIs(t, err, categorydomain.ErrNotFound)
	require.Equal(t, 5, cache.Invalidations(readcache.Categories), "failed writes keep the cache")
}
