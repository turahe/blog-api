package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type fakePostRepo struct {
	posts            map[uuid.UUID]postdomain.Post
	listAdminFilter  postdomain.AdminListFilter
	listAdminResult  postdomain.ListResult
	listAdminErr     error
	slugTakenBySlug  map[string]bool
	slugTakenErr     error
	updateCalls      int
	updatedPost      postdomain.Post
	setCoverImageErr error
}

func newFakePostRepo(posts ...postdomain.Post) *fakePostRepo {
	items := make(map[uuid.UUID]postdomain.Post, len(posts))
	for _, post := range posts {
		items[post.ID] = post
	}
	return &fakePostRepo{
		posts:           items,
		slugTakenBySlug: map[string]bool{},
	}
}

func (f *fakePostRepo) ListPublished(context.Context, postdomain.ListFilter) (postdomain.ListResult, error) {
	return postdomain.ListResult{}, nil
}

func (f *fakePostRepo) ListAdmin(_ context.Context, filter postdomain.AdminListFilter) (postdomain.ListResult, error) {
	f.listAdminFilter = filter
	return f.listAdminResult, f.listAdminErr
}

func (f *fakePostRepo) GetPublishedBySlug(context.Context, string) (postdomain.Post, error) {
	return postdomain.Post{}, postdomain.ErrNotFound
}

func (f *fakePostRepo) GetByID(_ context.Context, id uuid.UUID) (postdomain.Post, error) {
	post, ok := f.posts[id]
	if !ok {
		return postdomain.Post{}, postdomain.ErrNotFound
	}
	return post, nil
}

func (f *fakePostRepo) Create(_ context.Context, post postdomain.Post) (postdomain.Post, error) {
	f.posts[post.ID] = post
	return post, nil
}

func (f *fakePostRepo) Update(_ context.Context, post postdomain.Post) (postdomain.Post, error) {
	f.updateCalls++
	f.updatedPost = post
	f.posts[post.ID] = post
	return post, nil
}

func (f *fakePostRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	if f.slugTakenErr != nil {
		return false, f.slugTakenErr
	}
	post, ok := f.posts[excludeID]
	if ok && post.Slug == slug {
		return false, nil
	}
	return f.slugTakenBySlug[slug], nil
}

func (f *fakePostRepo) SetCoverImage(context.Context, uuid.UUID, *uuid.UUID, time.Time) error {
	return f.setCoverImageErr
}

func TestPostServiceListAdminClampsAndScopesAuthorFilter(t *testing.T) {
	t.Parallel()

	authorID := uuid.New()
	scopeAuthorID := uuid.New()
	want := postdomain.ListResult{Total: 3, Page: 1, PerPage: 20}
	repo := newFakePostRepo()
	repo.listAdminResult = want
	svc := New(repo, nil, fixedClock{})

	got, err := svc.ListAdmin(context.Background(), postdomain.AdminListFilter{
		Page:          0,
		PerPage:       101,
		Status:        "  PUBLISHED  ",
		AuthorID:      &authorID,
		Query:         "  hello world  ",
		ScopeAuthorID: &scopeAuthorID,
	})

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 1, repo.listAdminFilter.Page)
	require.Equal(t, 20, repo.listAdminFilter.PerPage)
	require.Equal(t, "published", repo.listAdminFilter.Status)
	require.Equal(t, "hello world", repo.listAdminFilter.Query)
	require.Equal(t, &scopeAuthorID, repo.listAdminFilter.AuthorID)
}

func TestPostServiceUpdateOwnPostRestrictedActorSucceedsAndIncrementsVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 21, 0, 0, 0, time.UTC)
	postID := uuid.New()
	actorID := uuid.New()
	categoryID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		ID:         postID,
		AuthorID:   actorID,
		CategoryID: &categoryID,
		Title:      "Old title",
		Slug:       "old-slug",
		Excerpt:    "Old excerpt",
		Content:    "Old content",
		Status:     postdomain.StatusDraft,
		Version:    2,
		CreatedAt:  now.Add(-time.Hour),
		UpdatedAt:  now.Add(-time.Hour),
	})
	svc := New(repo, nil, fixedClock{now: now})

	title := "  New title  "
	slug := "  New-Slug  "
	excerpt := "  New excerpt  "
	content := "New content"
	got, err := svc.Update(context.Background(), postID, actorID, false, postdomain.UpdateInput{
		Title:   &title,
		Slug:    &slug,
		Excerpt: &excerpt,
		Content: &content,
		CategoryID: postdomain.OptionalCategoryID{
			Present: true,
			Value:   nil,
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(3), got.Version)
	require.Equal(t, "New title", got.Title)
	require.Equal(t, "new-slug", got.Slug)
	require.Equal(t, "New excerpt", got.Excerpt)
	require.Equal(t, "New content", got.Content)
	require.Nil(t, got.CategoryID)
	require.Equal(t, now, got.UpdatedAt)
	require.Equal(t, 1, repo.updateCalls)
}

func TestPostServiceUpdateOtherAuthorsPostRestrictedActorReturnsNotFound(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		ID:       postID,
		AuthorID: uuid.New(),
		Title:    "Title",
		Slug:     "slug",
		Version:  1,
	})
	svc := New(repo, nil, fixedClock{now: time.Now()})

	title := "Updated"
	_, err := svc.Update(context.Background(), postID, uuid.New(), false, postdomain.UpdateInput{
		Title: &title,
	})

	require.ErrorIs(t, err, postdomain.ErrNotFound)
	require.Equal(t, 0, repo.updateCalls)
}

func TestPostServiceUpdateRejectsInvalidSlug(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	actorID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		ID:       postID,
		AuthorID: actorID,
		Title:    "Title",
		Slug:     "slug",
		Version:  1,
	})
	svc := New(repo, nil, fixedClock{now: time.Now()})

	slug := "not valid"
	_, err := svc.Update(context.Background(), postID, actorID, false, postdomain.UpdateInput{
		Slug: &slug,
	})

	require.ErrorIs(t, err, ErrValidation)
	require.Equal(t, 0, repo.updateCalls)
}

func TestPostServiceUpdateReturnsConflictWhenSlugTaken(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	actorID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		ID:       postID,
		AuthorID: actorID,
		Title:    "Title",
		Slug:     "slug",
		Version:  1,
	})
	repo.slugTakenBySlug["taken-slug"] = true
	svc := New(repo, nil, fixedClock{now: time.Now()})

	slug := "taken-slug"
	_, err := svc.Update(context.Background(), postID, actorID, false, postdomain.UpdateInput{
		Slug: &slug,
	})

	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, 0, repo.updateCalls)
}

func TestPostServiceUpdateRejectsEmptyUpdate(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	actorID := uuid.New()
	repo := newFakePostRepo(postdomain.Post{
		ID:       postID,
		AuthorID: actorID,
		Title:    "Title",
		Slug:     "slug",
		Version:  1,
	})
	svc := New(repo, nil, fixedClock{now: time.Now()})

	_, err := svc.Update(context.Background(), postID, actorID, false, postdomain.UpdateInput{})

	require.ErrorIs(t, err, ErrValidation)
	require.Equal(t, 0, repo.updateCalls)
}

func TestPostServiceUpdateSoftDeletedPostReturnsNotFound(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	actorID := uuid.New()
	deletedAt := time.Now()
	repo := newFakePostRepo(postdomain.Post{
		ID:        postID,
		AuthorID:  actorID,
		Title:     "Title",
		Slug:      "slug",
		Version:   1,
		DeletedAt: &deletedAt,
	})
	svc := New(repo, nil, fixedClock{now: time.Now()})

	title := "Updated"
	_, err := svc.Update(context.Background(), postID, actorID, false, postdomain.UpdateInput{
		Title: &title,
	})

	require.ErrorIs(t, err, postdomain.ErrNotFound)
	require.Equal(t, 0, repo.updateCalls)
}
