package service

import (
	"context"
	"errors"
	"strings"
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

	slugErr      error
	getBySlugErr error
	createErr    error
	countErr     error
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
	if f.getBySlugErr != nil {
		return tagdomain.Tag{}, f.getBySlugErr
	}

	tag, ok := f.bySlug[slug]
	if !ok {
		return tagdomain.Tag{}, tagdomain.ErrNotFound
	}

	return tag, nil
}

func (f *fakeRepo) Create(_ context.Context, tag tagdomain.Tag) (tagdomain.Tag, error) {
	if f.createErr != nil {
		return tagdomain.Tag{}, f.createErr
	}

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
	if f.slugErr != nil {
		return false, f.slugErr
	}

	tag, ok := f.bySlug[slug]
	if !ok {
		return false, nil
	}

	return tag.UUID != excludeID, nil
}

func (f *fakeRepo) CountPosts(_ context.Context, tagID uuid.UUID) (int64, error) {
	return f.counts[tagID], f.countErr
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

func TestCreateFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		tagName string
		slug    string
		breakIt func(*fakeRepo)
		wantErr error
	}{
		{name: "name too long", tagName: strings.Repeat("n", 65), breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "name without slug characters", tagName: "!!!", breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "slug check fails", tagName: "Go", breakIt: func(r *fakeRepo) { r.slugErr = boom }, wantErr: boom},
		{name: "insert fails", tagName: "Go", breakIt: func(r *fakeRepo) { r.createErr = boom }, wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo()
			tt.breakIt(repo)
			svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

			_, err := svc.Create(t.Context(), tt.tagName, tt.slug)
			require.ErrorIs(t, err, tt.wantErr)
			require.Empty(t, repo.byID)
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()

	tests := []struct {
		name     string
		tagName  *string
		slug     *string
		wantName string
		wantSlug string
	}{
		{name: "rename", tagName: new("  Golang "), wantName: "Golang", wantSlug: "go"},
		{name: "unchanged slug", slug: new(" GO "), wantName: "Go", wantSlug: "go"},
		{name: "free slug", slug: new("golang"), wantName: "Go", wantSlug: "golang"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Go", Slug: "go"})
			svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

			got, err := svc.Update(t.Context(), tagID, tt.tagName, tt.slug)
			require.NoError(t, err)
			require.Equal(t, tt.wantName, got.Name)
			require.Equal(t, tt.wantSlug, got.Slug)
			require.Equal(t, got, repo.byID[tagID])
		})
	}
}

func TestUpdateFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tagID := uuid.New()

	tests := []struct {
		name    string
		id      uuid.UUID
		tagName *string
		slug    *string
		breakIt func(*fakeRepo)
		wantErr error
	}{
		{name: "nothing to change", id: tagID, breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "unknown tag", id: uuid.New(), tagName: new("x"), breakIt: func(*fakeRepo) {}, wantErr: tagdomain.ErrNotFound},
		{name: "blank name", id: tagID, tagName: new("  "), breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "name too long", id: tagID, tagName: new(strings.Repeat("n", 65)), breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "blank slug", id: tagID, slug: new(" "), breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "slug check fails", id: tagID, slug: new("golang"), breakIt: func(r *fakeRepo) { r.slugErr = boom }, wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := tagdomain.Tag{UUID: tagID, Name: "Go", Slug: "go"}
			repo := newFakeRepo(original)
			tt.breakIt(repo)
			svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

			_, err := svc.Update(t.Context(), tt.id, tt.tagName, tt.slug)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, original, repo.byID[tagID])
		})
	}
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

func TestDeletePassesThroughCountFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tagID := uuid.New()
	repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Tag", Slug: "tag"})
	repo.countErr = boom
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	require.ErrorIs(t, svc.Delete(t.Context(), tagID), boom)
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

func TestMergeRequiresBothTags(t *testing.T) {
	t.Parallel()

	tagID := uuid.New()

	tests := []struct {
		name         string
		source, into uuid.UUID
	}{
		{name: "unknown source", source: uuid.New(), into: tagID},
		{name: "unknown target", source: tagID, into: uuid.New()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo(tagdomain.Tag{UUID: tagID, Name: "Tag", Slug: "tag"})
			svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

			require.ErrorIs(t, svc.Merge(t.Context(), tt.source, tt.into), tagdomain.ErrNotFound)
			require.Empty(t, repo.merged)
		})
	}
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

func TestResolveOrCreateSkipsBlankNames(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	got, err := svc.ResolveOrCreate(t.Context(), []string{"", "   "})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Empty(t, repo.byID)
}

func TestResolveOrCreateFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		names   []string
		breakIt func(*fakeRepo)
		wantErr error
	}{
		{name: "name too long", names: []string{strings.Repeat("n", 65)}, breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "name without slug characters", names: []string{"???"}, breakIt: func(*fakeRepo) {}, wantErr: ErrValidation},
		{name: "lookup fails", names: []string{"Go"}, breakIt: func(r *fakeRepo) { r.getBySlugErr = boom }, wantErr: boom},
		{name: "create fails", names: []string{"Go"}, breakIt: func(r *fakeRepo) { r.createErr = boom }, wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo()
			tt.breakIt(repo)
			svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

			got, err := svc.ResolveOrCreate(t.Context(), tt.names)
			require.ErrorIs(t, err, tt.wantErr)
			require.Nil(t, got)
		})
	}
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

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "spaces", in: "Hello World", want: "hello-world"},
		{name: "punctuation dropped", in: "C++ & Go!", want: "c-go"},
		{name: "separators collapse", in: " a -_ b ", want: "a-b"},
		{name: "nothing usable", in: "***", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, slugify(tt.in))
		})
	}
}
