package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

var lifecycleNow = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

func lifecyclePost(status postdomain.Status) postdomain.Post {
	published := lifecycleNow.Add(-time.Hour)

	post := postdomain.Post{UUID: uuid.New(), Slug: "hello", Status: status, Version: 3}
	if status == postdomain.StatusPublished || status == postdomain.StatusArchived {
		post.PublishedAt = &published
	}

	return post
}

func TestTransitionTable(t *testing.T) {
	t.Parallel()

	statuses := []postdomain.Status{
		postdomain.StatusDraft, postdomain.StatusScheduled, postdomain.StatusPublished, postdomain.StatusArchived,
	}
	allowed := map[postdomain.Transition]map[postdomain.Status]bool{
		postdomain.TransitionPublish:   {postdomain.StatusDraft: true, postdomain.StatusScheduled: true, postdomain.StatusArchived: true},
		postdomain.TransitionUnpublish: {postdomain.StatusPublished: true, postdomain.StatusScheduled: true},
		postdomain.TransitionArchive:   {postdomain.StatusDraft: true, postdomain.StatusScheduled: true, postdomain.StatusPublished: true},
	}

	for transition, from := range allowed {
		for _, status := range statuses {
			_, err := transition.Next(status)
			if from[status] {
				require.NoError(t, err, "%s from %s", transition, status)
			} else {
				require.ErrorIs(t, err, postdomain.ErrInvalidTransition, "%s from %s", transition, status)
			}
		}
	}

	_, err := postdomain.Transition("delete").Next(postdomain.StatusDraft)
	require.ErrorIs(t, err, postdomain.ErrValidation)
}

func TestPublishUnpublishArchive(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusDraft)
	repo := newFakePostRepo(post)
	svc := New(repo, nil, fixedClock{now: lifecycleNow})

	got, err := svc.Publish(context.Background(), post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.StatusPublished, got.Status)
	require.Equal(t, lifecycleNow, *got.PublishedAt)
	require.Equal(t, int64(4), got.Version)

	_, err = svc.Publish(context.Background(), post.UUID)
	require.ErrorIs(t, err, postdomain.ErrInvalidTransition)

	got, err = svc.Archive(context.Background(), post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.StatusArchived, got.Status)
	require.Equal(t, lifecycleNow, *got.PublishedAt, "archive keeps published_at")

	_, err = svc.Unpublish(context.Background(), post.UUID)
	require.ErrorIs(t, err, postdomain.ErrInvalidTransition)

	_, err = svc.Publish(context.Background(), post.UUID)
	require.NoError(t, err)

	got, err = svc.Unpublish(context.Background(), post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.StatusDraft, got.Status)
	require.Nil(t, got.PublishedAt)
}

func TestTransitionsOnMissingPostReturnNotFound(t *testing.T) {
	t.Parallel()

	svc := New(newFakePostRepo(), nil, fixedClock{now: lifecycleNow})

	for name, apply := range map[string]func(context.Context, uuid.UUID) (postdomain.Post, error){
		"publish": svc.Publish, "unpublish": svc.Unpublish, "archive": svc.Archive, "restore": svc.Restore,
	} {
		_, err := apply(context.Background(), uuid.New())
		require.ErrorIs(t, err, postdomain.ErrNotFound, name)
	}

	require.ErrorIs(t, svc.Delete(context.Background(), uuid.New()), postdomain.ErrNotFound)
}

func TestDeleteThenRestoreComesBackAsDraft(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusPublished)
	repo := newFakePostRepo(post)
	svc := New(repo, nil, fixedClock{now: lifecycleNow})

	require.NoError(t, svc.Delete(context.Background(), post.UUID))
	require.Equal(t, lifecycleNow, *repo.posts[post.UUID].DeletedAt)
	require.ErrorIs(t, svc.Delete(context.Background(), post.UUID), postdomain.ErrNotFound)

	_, err := svc.Publish(context.Background(), post.UUID)
	require.ErrorIs(t, err, postdomain.ErrNotFound, "deleted posts are hidden from transitions")

	got, err := svc.Restore(context.Background(), post.UUID)
	require.NoError(t, err)
	require.Nil(t, got.DeletedAt)
	require.Equal(t, postdomain.StatusDraft, got.Status)
	require.Nil(t, got.PublishedAt)
	require.Equal(t, "hello", got.Slug)
	require.Equal(t, int64(5), got.Version)

	_, err = svc.Restore(context.Background(), post.UUID)
	require.ErrorIs(t, err, postdomain.ErrNotFound, "only deleted posts can be restored")
}

func TestRestoreSuffixesSlugTakenMeanwhile(t *testing.T) {
	t.Parallel()

	deletedAt := lifecycleNow.Add(-time.Hour)
	deleted := lifecyclePost(postdomain.StatusDraft)
	deleted.DeletedAt = &deletedAt
	live := postdomain.Post{UUID: uuid.New(), Slug: "hello", Status: postdomain.StatusPublished}
	liveTwo := postdomain.Post{UUID: uuid.New(), Slug: "hello-2", Status: postdomain.StatusDraft}
	repo := newFakePostRepo(deleted, live, liveTwo)
	svc := New(repo, nil, fixedClock{now: lifecycleNow})

	got, err := svc.Restore(context.Background(), deleted.UUID)

	require.NoError(t, err)
	require.Equal(t, "hello-3", got.Slug)
}

func TestRestoreRetriesWhenSlugClaimedConcurrently(t *testing.T) {
	t.Parallel()

	deletedAt := lifecycleNow
	deleted := lifecyclePost(postdomain.StatusDraft)
	deleted.DeletedAt = &deletedAt
	repo := newFakePostRepo(deleted)
	repo.restoreConflicts = 1
	svc := New(repo, nil, fixedClock{now: lifecycleNow})

	got, err := svc.Restore(context.Background(), deleted.UUID)

	require.NoError(t, err)
	require.Equal(t, 2, repo.restoreCalls)
	require.Equal(t, "hello-2", got.Slug)
}

func TestCreateDraftDerivesAndSuffixesSlug(t *testing.T) {
	t.Parallel()

	author := uuid.New()
	repo := newFakePostRepo(postdomain.Post{UUID: uuid.New(), Slug: "hello-world"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: lifecycleNow})

	got, _, err := svc.CreateDraft(context.Background(), author, "  Hello, World!  ", "", "", "body", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "hello-world-2", got.Slug)

	svc = New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: lifecycleNow})
	got, _, err = svc.CreateDraft(context.Background(), author, "Other", "hello-world", "", "body", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "hello-world-3", got.Slug, "explicit slugs are suffixed too")

	_, _, err = svc.CreateDraft(context.Background(), author, "¡¿!", "", "", "body", nil, nil)
	require.ErrorIs(t, err, ErrValidation)

	_, _, err = svc.CreateDraft(context.Background(), author, "Title", "Not A Slug", "", "body", nil, nil)
	require.ErrorIs(t, err, ErrValidation)
}

func TestFreeSlugIgnoresUnrelatedPrefixesAndKeepsLength(t *testing.T) {
	t.Parallel()

	repo := newFakePostRepo(
		postdomain.Post{UUID: uuid.New(), Slug: "go"},
		postdomain.Post{UUID: uuid.New(), Slug: "go-tips"},
		postdomain.Post{UUID: uuid.New(), Slug: "gopher"},
	)
	svc := New(repo, nil, fixedClock{})

	got, err := svc.freeSlug(context.Background(), "go")
	require.NoError(t, err)
	require.Equal(t, "go-2", got)

	long := strings.Repeat("a", postdomain.MaxSlugLength)
	require.Len(t, withSuffix(long, 12), postdomain.MaxSlugLength)
	require.Equal(t, strings.Repeat("a", postdomain.MaxSlugLength-3)+"-12", withSuffix(long, 12))
	require.Equal(t, "ab-2", withSuffix("ab", 2))
	require.Equal(t, strings.Repeat("a", postdomain.MaxSlugLength-3)+"-10",
		withSuffix(strings.Repeat("a", postdomain.MaxSlugLength-3)+"-b", 10), "no dangling hyphen after trimming")
}
