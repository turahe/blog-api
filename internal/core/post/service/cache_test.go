package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
)

func TestPublicReadsAreCachedPerNormalisedQuery(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusPublished)
	repo := newFakePostRepo(post)
	cache := readcachetest.New()
	svc := New(repo, nil, fixedClock{now: lifecycleNow}).WithCache(cache)
	ctx := context.Background()

	for range 2 {
		_, err := svc.ListPublished(ctx, postdomain.ListFilter{Page: 0, PerPage: 500})
		require.NoError(t, err)
		_, err = svc.ListPublished(ctx, postdomain.ListFilter{Page: 1, PerPage: 20})
		require.NoError(t, err)

		got, err := svc.GetPublishedBySlug(ctx, " hello ")
		require.NoError(t, err)
		require.Equal(t, post.UUID, got.UUID)
	}

	require.Equal(t, 2, repo.publicReads, "invalid paging normalises onto the default page key")
	require.ElementsMatch(t, []string{
		"list:page=1:per_page=20:category=-:tag=-",
		"get:slug=hello",
	}, cache.Keys(readcache.Posts))

	_, err := svc.GetPublishedBySlug(ctx, "missing")
	require.ErrorIs(t, err, postdomain.ErrNotFound)
	_, err = svc.GetPublishedBySlug(ctx, "missing")
	require.ErrorIs(t, err, postdomain.ErrNotFound)
	require.Equal(t, 4, repo.publicReads, "misses are not cached")

	_, err = svc.ListPublished(readcache.WithBypass(ctx), postdomain.ListFilter{Page: 1, PerPage: 20})
	require.NoError(t, err)
	require.Equal(t, 5, repo.publicReads, "bypass reads the repository")
}

func TestPostWritesInvalidatePublicReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	writes := map[string]func(*PostService, postdomain.Post) error{
		"publish": func(s *PostService, p postdomain.Post) error {
			_, err := s.Publish(ctx, p.UUID)
			return err
		},
		"unpublish": func(s *PostService, p postdomain.Post) error {
			_, err := s.Unpublish(ctx, p.UUID)
			return err
		},
		"archive": func(s *PostService, p postdomain.Post) error {
			_, err := s.Archive(ctx, p.UUID)
			return err
		},
		"delete": func(s *PostService, p postdomain.Post) error { return s.Delete(ctx, p.UUID) },
		"update": func(s *PostService, p postdomain.Post) error {
			title := "New title"
			_, _, err := s.Update(ctx, p.UUID, uuid.New(), true, postdomain.UpdateInput{Title: &title})

			return err
		},
	}

	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := postdomain.StatusPublished
			if name == "publish" {
				status = postdomain.StatusDraft
			}

			post := lifecyclePost(status)
			cache := readcachetest.New()
			svc := New(newFakePostRepo(post), nil, fixedClock{now: lifecycleNow}).WithCache(cache)

			require.NoError(t, write(svc, post))
			require.Equal(t, 1, cache.Invalidations(readcache.Posts))
		})
	}
}

func TestFailedPostWritesKeepCache(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusDraft)
	cache := readcachetest.New()
	svc := New(newFakePostRepo(post), nil, fixedClock{now: lifecycleNow}).WithCache(cache)

	_, err := svc.Unpublish(context.Background(), post.UUID)
	require.ErrorIs(t, err, postdomain.ErrInvalidTransition)
	require.ErrorIs(t, svc.Delete(context.Background(), uuid.New()), postdomain.ErrNotFound)
	require.Zero(t, cache.Invalidations(readcache.Posts))
}
