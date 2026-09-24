package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
)

func TestDeleteInvalidatesPostAndCategoryReads(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()
	cache := readcachetest.New()
	svc.WithCache(cache)

	asset := repo.store(makePendingAsset())

	require.NoError(t, svc.Delete(context.Background(), asset.UUID))
	require.Equal(t, 1, cache.Invalidations(readcache.Posts))
	require.Equal(t, 1, cache.Invalidations(readcache.Categories))

	require.ErrorIs(t, svc.Delete(context.Background(), uuid.New()), ErrNotFound)
	require.Equal(t, 1, cache.Invalidations(readcache.Posts))
}
