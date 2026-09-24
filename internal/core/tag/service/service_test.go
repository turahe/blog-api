package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type fixedIDs struct {
	next uuid.UUID
}

func (f fixedIDs) New() uuid.UUID {
	return f.next
}

type fakeRepo struct {
	byID    map[uuid.UUID]tagdomain.Tag
	bySlug  map[string]tagdomain.Tag
	counts  map[uuid.UUID]int64
	merged  [][2]uuid.UUID
	deleted []uuid.UUID
}

func newFakeRepo(tags ...tagdomain.Tag) *fakeRepo {
	byID := make(map[uuid.UUID]tagdomain.Tag, len(tags))
	bySlug := make(map[string]tagdomain.Tag, len(tags))
	for _, tag := range tags {
		byID[tag.UUID] = tag
		bySlug[tag.Slug] = tag
	}
	return &fakeRepo{
		byID:   byID,
		bySlug: bySlug,
		counts: map[uuid.UUID]int64{},
	}
}

func (f *fakeRepo) List(_ context.Context) ([]tagdomain.Tag, error) {
	out := make([]tagdomain.Tag, 0, len(f.byID))
	for _, tag := range f.byID {
		out = append(out, tag)
	}
	return out, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (tagdomain.Tag, error) {
	tag, ok := f.byID[id]
	if !ok {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}
	return tag, nil
}

func (f *fakeRepo) GetBySlug(_ context.Context, slug string) (tagdomain.Tag, error) {
	tag, ok := f.bySlug[slug]
	if !ok {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}
	return tag, nil
}

func (f *fakeRepo) Create(_ context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	f.byID[tag.UUID] = tag
	f.bySlug[tag.Slug] = tag
	return tag, nil
}

func (f *fakeRepo) Update(_ context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	old, ok := f.byID[tag.UUID]
	if ok && old.Slug != tag.Slug {
		delete(f.bySlug, old.Slug)
	}
	f.byID[tag.UUID] = tag
	f.bySlug[tag.Slug] = tag
	return tag, nil
}

func (f *fakeRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	tag, ok := f.bySlug[slug]
	if !ok {
		return false, nil
	}
	return tag.UUID != excludeID, nil
}

func (f *fakeRepo) CountPosts(_ context.Context, tagID uuid.UUID) (int64, error) {
	return f.counts[tagID], nil
}

func (f *fakeRepo) MergeInto(_ context.Context, sourceID, targetID uuid.UUID) error {
	f.merged = append(f.merged, [2]uuid.UUID{sourceID, targetID})
	source, ok := f.byID[sourceID]
	if ok {
		delete(f.bySlug, source.Slug)
		delete(f.byID, sourceID)
	}
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	tag, ok := f.byID[id]
	if !ok {
		return tagdomain.ErrNotFound
	}
	delete(f.bySlug, tag.Slug)
	delete(f.byID, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeRepo) ReplacePostTags(context.Context, uuid.UUID, []uuid.UUID) error {
	return nil
}

func (f *fakeRepo) ListByPostID(context.Context, uuid.UUID) ([]tagdomain.Tag, error) {
	return nil, nil
}

func TestCreateSlugifiesNameAndRejectsEmptyName(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	newID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := newFakeRepo()
	svc := New(repo, fixedIDs{next: newID}, fixedClock{now: now})

	got, err := svc.Create(context.Background(), "  Hello World  ", "")
	require.NoError(t, err)
	require.Equal(t, "Hello World", got.Name)
	require.Equal(t, "hello-world", got.Slug)
	require.Equal(t, newID, got.UUID)
	require.Equal(t, now, got.CreatedAt)

	_, err = svc.Create(context.Background(), "   ", "")
	require.ErrorIs(t, err, ErrValidation)
}

func TestCreateRejectsInvalidExplicitSlug(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Create(context.Background(), "Tag", "Bad Slug")
	require.ErrorIs(t, err, ErrValidation)
}

func TestUpdateRejectsInvalidExplicitSlug(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Tag", Slug: "tag"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	slug := "bad_slug"
	_, err := svc.Update(context.Background(), tagID, nil, &slug)
	require.ErrorIs(t, err, ErrValidation)
}

func TestCreateReturnsConflictWhenSlugTaken(t *testing.T) {
	t.Parallel()

	existingID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{
		UUID: existingID,
		Name: "Taken",
		Slug: "taken-slug",
	})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Create(context.Background(), "Another", "taken-slug")
	require.ErrorIs(t, err, ErrConflict)
}

func TestUpdateReturnsConflictWhenSlugTaken(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	otherID := uuid.New()
	repo := newFakeRepo(
		tagdomain.Tag{UUID: tagID, Name: "Mine", Slug: "mine"},
		tagdomain.Tag{UUID: otherID, Name: "Other", Slug: "other-slug"},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	slug := "other-slug"
	_, err := svc.Update(context.Background(), tagID, nil, &slug)
	require.ErrorIs(t, err, ErrConflict)
}

func TestDeleteReturnsInUseWhenPostsAttached(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Used", Slug: "used"})
	repo.counts[tagID] = 2
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Delete(context.Background(), tagID)
	require.ErrorIs(t, err, tagdomain.ErrInUse)
	require.Empty(t, repo.deleted)
}

func TestDeleteCallsRepoWhenNoPostsAttached(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Free", Slug: "free"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Delete(context.Background(), tagID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{tagID}, repo.deleted)
}

func TestMergeSameIDReturnsValidationError(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Tag", Slug: "tag"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Merge(context.Background(), tagID, tagID)
	require.ErrorIs(t, err, ErrValidation)
	require.Empty(t, repo.merged)
}

func TestMergeSuccessCallsMergeInto(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	targetID := uuid.New()
	repo := newFakeRepo(
		tagdomain.Tag{UUID: sourceID, Name: "Source", Slug: "source"},
		tagdomain.Tag{UUID: targetID, Name: "Target", Slug: "target"},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Merge(context.Background(), sourceID, targetID)
	require.NoError(t, err)
	require.Equal(t, [][2]uuid.UUID{{sourceID, targetID}}, repo.merged)
}

func TestResolveOrCreateDedupesCreatesMissingReturnsExisting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	existingID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	newID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	repo := newFakeRepo(tagdomain.Tag{
		UUID:      existingID,
		Name:      "Go",
		Slug:      "go",
		CreatedAt: now,
	})
	svc := New(repo, fixedIDs{next: newID}, fixedClock{now: now})

	got, err := svc.ResolveOrCreate(context.Background(), []string{
		"Go",
		"  go  ",
		"Rust Lang",
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, existingID, got[0].UUID)
	require.Equal(t, "go", got[0].Slug)
	require.Equal(t, newID, got[1].UUID)
	require.Equal(t, "Rust Lang", got[1].Name)
	require.Equal(t, "rust-lang", got[1].Slug)
}

func TestReplacePostTagsDelegatesToRepo(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	tagID := uuid.New()
	repo := newFakeRepo()
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.ReplacePostTags(context.Background(), postID, []uuid.UUID{tagID})
	require.NoError(t, err)
}

func TestListByPostIDDelegatesToRepo(t *testing.T) {
	t.Parallel()

	postID := uuid.New()
	repo := newFakeRepo()
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	got, err := svc.ListByPostID(context.Background(), postID)
	require.NoError(t, err)
	require.Nil(t, got)
}
