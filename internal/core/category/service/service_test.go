package service

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	"github.com/turahe/blog-api/internal/core/category/ports"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type fixedIDs struct {
	next []uuid.UUID
}

func (f *fixedIDs) New() uuid.UUID {
	if len(f.next) == 0 {
		return uuid.New()
	}
	id := f.next[0]
	f.next = f.next[1:]
	return id
}

type fakeRepo struct {
	byID     map[uuid.UUID]categorydomain.Category
	bySlug   map[string]categorydomain.Category
	posts    map[uuid.UUID]int64
	deleted  []uuid.UUID
	rebuilds int
}

func newFakeRepo(cats ...categorydomain.Category) *fakeRepo {
	repo := &fakeRepo{
		byID:   map[uuid.UUID]categorydomain.Category{},
		bySlug: map[string]categorydomain.Category{},
		posts:  map[uuid.UUID]int64{},
	}
	for _, cat := range cats {
		repo.put(cat)
	}
	return repo
}

func (f *fakeRepo) put(cat categorydomain.Category) {
	f.byID[cat.ID] = cat
	f.bySlug[cat.Slug] = cat
}

func (f *fakeRepo) List(context.Context) ([]categorydomain.Category, error) {
	out := make([]categorydomain.Category, 0, len(f.byID))
	for _, cat := range f.byID {
		out = append(out, cat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lft != out[j].Lft {
			return out[i].Lft < out[j].Lft
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (categorydomain.Category, error) {
	cat, ok := f.byID[id]
	if !ok {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return cat, nil
}

func (f *fakeRepo) GetBySlug(_ context.Context, slug string) (categorydomain.Category, error) {
	cat, ok := f.bySlug[slug]
	if !ok {
		return categorydomain.Category{}, categorydomain.ErrNotFound
	}
	return cat, nil
}

func (f *fakeRepo) Create(_ context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	f.put(cat)
	return cat, nil
}

func (f *fakeRepo) Update(_ context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	old, ok := f.byID[cat.ID]
	if ok && old.Slug != cat.Slug {
		delete(f.bySlug, old.Slug)
	}
	f.put(cat)
	return cat, nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	cat, ok := f.byID[id]
	if !ok {
		return categorydomain.ErrNotFound
	}
	delete(f.byID, id)
	delete(f.bySlug, cat.Slug)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	cat, ok := f.bySlug[slug]
	if !ok {
		return false, nil
	}
	return cat.ID != excludeID, nil
}

func (f *fakeRepo) CountPosts(_ context.Context, categoryID uuid.UUID) (int64, error) {
	return f.posts[categoryID], nil
}

func (f *fakeRepo) CountChildren(_ context.Context, categoryID uuid.UUID) (int64, error) {
	var n int64
	for _, cat := range f.byID {
		if cat.ParentID != nil && *cat.ParentID == categoryID {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) ReplaceTreeBounds(_ context.Context, cats []categorydomain.Category) error {
	f.rebuilds++
	for _, cat := range cats {
		old, ok := f.byID[cat.ID]
		if ok && old.Slug != cat.Slug {
			delete(f.bySlug, old.Slug)
		}
		f.put(cat)
	}
	return nil
}

func (f *fakeRepo) WithinTx(ctx context.Context, fn func(context.Context, ports.Repository) error) error {
	return fn(ctx, f)
}

func TestCreateSlugifiesNameRejectsEmptyAndConflicts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	newID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ids := &fixedIDs{next: []uuid.UUID{newID}}
	repo := newFakeRepo()
	svc := New(repo, ids, fixedClock{now: now})

	got, err := svc.Create(ctx, CreateInput{Name: "  Hello World  "})
	require.NoError(t, err)
	require.Equal(t, newID, got.ID)
	require.Equal(t, "Hello World", got.Name)
	require.Equal(t, "hello-world", got.Slug)
	require.Equal(t, 1, got.Lft)
	require.Equal(t, 2, got.Rgt)
	require.Equal(t, 0, got.Depth)
	require.Equal(t, now, got.CreatedAt)
	require.Equal(t, now, got.UpdatedAt)

	_, err = svc.Create(ctx, CreateInput{Name: "   "})
	require.ErrorIs(t, err, ErrValidation)

	_, err = svc.Create(ctx, CreateInput{Name: "Another", Slug: "hello-world"})
	require.ErrorIs(t, err, categorydomain.ErrConflict)
}

func TestCreateWithParentAndBeforePlacesSiblingOrderAndRebuilds(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	parentID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	firstID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	secondID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	newID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{ID: parentID, Name: "Parent", Slug: "parent", Lft: 1, Rgt: 6, SortOrder: 0},
		categorydomain.Category{ID: firstID, Name: "Apple", Slug: "apple", ParentID: &parentID, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0},
		categorydomain.Category{ID: secondID, Name: "Banana", Slug: "banana", ParentID: &parentID, Lft: 4, Rgt: 5, Depth: 1, SortOrder: 1},
	)
	svc := New(repo, &fixedIDs{next: []uuid.UUID{newID}}, fixedClock{now: now})

	got, err := svc.Create(ctx, CreateInput{Name: "Cherry", ParentID: &parentID, BeforeID: &secondID})
	require.NoError(t, err)
	require.Equal(t, newID, got.ID)
	require.Equal(t, 1, got.SortOrder)
	require.Equal(t, 4, got.Lft)
	require.Equal(t, 5, got.Rgt)
	require.Equal(t, 1, got.Depth)

	first := repo.byID[firstID]
	second := repo.byID[secondID]
	parent := repo.byID[parentID]
	require.Equal(t, 0, first.SortOrder)
	require.Equal(t, 2, second.SortOrder)
	require.Equal(t, 1, parent.Lft)
	require.Equal(t, 8, parent.Rgt)
	require.Equal(t, 1, repo.rebuilds)
}

func TestUpdateRejectsSlugConflictAndUpdatesMetadataOnly(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	catID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	otherID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	parentID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	imageID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{ID: catID, Name: "Mine", Slug: "mine", Description: "old", ParentID: &parentID, ImageID: &imageID, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0},
		categorydomain.Category{ID: otherID, Name: "Other", Slug: "other"},
	)
	svc := New(repo, &fixedIDs{}, fixedClock{now: now})

	slug := "other"
	_, err := svc.Update(ctx, catID, UpdateInput{Slug: &slug})
	require.ErrorIs(t, err, categorydomain.ErrConflict)
	require.Equal(t, "mine", repo.byID[catID].Slug)

	name := " Renamed "
	description := ""
	got, err := svc.Update(ctx, catID, UpdateInput{
		Name:            &name,
		Description:     &description,
		ImageIDProvided: true,
		ImageID:         nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.Name)
	require.Equal(t, "", got.Description)
	require.Nil(t, got.ImageID)
	require.Equal(t, &parentID, got.ParentID)
	require.Equal(t, 2, got.Lft)
	require.Equal(t, 3, got.Rgt)
	require.Equal(t, now, got.UpdatedAt)
	require.Zero(t, repo.rebuilds)
}

func TestMoveRejectsOwnDescendantAndReparentsSuccessfully(t *testing.T) {
	ctx := context.Background()
	rootID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	childID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	grandID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	otherRootID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(rebuildBounds([]categorydomain.Category{
		{ID: rootID, Name: "Root", Slug: "root", SortOrder: 0},
		{ID: childID, Name: "Child", Slug: "child", ParentID: &rootID, SortOrder: 0},
		{ID: grandID, Name: "Grand", Slug: "grand", ParentID: &childID, SortOrder: 0},
		{ID: otherRootID, Name: "Other", Slug: "other", SortOrder: 1},
	})...)
	svc := New(repo, &fixedIDs{}, fixedClock{now: time.Date(2026, 8, 1, 4, 0, 0, 0, time.UTC)})

	_, err := svc.Move(ctx, childID, &grandID, nil)
	require.ErrorIs(t, err, ErrValidation)

	got, err := svc.Move(ctx, childID, &otherRootID, nil)
	require.NoError(t, err)
	require.Equal(t, &otherRootID, got.ParentID)
	require.Equal(t, 1, got.Depth)
	require.Equal(t, 2, repo.byID[grandID].Depth)
	require.True(t, repo.byID[otherRootID].Lft < got.Lft)
	require.True(t, got.Lft < repo.byID[grandID].Lft)
}

func TestDeleteBlocksInUseAndRebuildsAfterDelete(t *testing.T) {
	ctx := context.Background()
	rootID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	childID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	usedID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	freeID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(rebuildBounds([]categorydomain.Category{
		{ID: rootID, Name: "Root", Slug: "root"},
		{ID: childID, Name: "Child", Slug: "child", ParentID: &rootID},
		{ID: usedID, Name: "Used", Slug: "used", SortOrder: 1},
		{ID: freeID, Name: "Free", Slug: "free", SortOrder: 2},
	})...)
	repo.posts[usedID] = 1
	svc := New(repo, &fixedIDs{}, fixedClock{now: time.Now()})

	err := svc.Delete(ctx, rootID)
	require.ErrorIs(t, err, categorydomain.ErrInUse)
	err = svc.Delete(ctx, usedID)
	require.ErrorIs(t, err, categorydomain.ErrInUse)

	err = svc.Delete(ctx, freeID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{freeID}, repo.deleted)
	require.NotContains(t, repo.byID, freeID)
	require.Equal(t, 6, repo.byID[usedID].Rgt)
	require.Equal(t, 1, repo.rebuilds)
}

func TestRebuildAllAssignsStableNestedSetOrder(t *testing.T) {
	ctx := context.Background()
	rootA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	rootB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	childB := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	childA := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{ID: childA, Name: "Alpha", Slug: "alpha", ParentID: &rootA, SortOrder: 10},
		categorydomain.Category{ID: rootB, Name: "Root B", Slug: "root-b", SortOrder: 1},
		categorydomain.Category{ID: rootA, Name: "Root A", Slug: "root-a", SortOrder: 0},
		categorydomain.Category{ID: childB, Name: "Beta", Slug: "beta", ParentID: &rootA, SortOrder: 10},
	)
	svc := New(repo, &fixedIDs{}, fixedClock{now: time.Now()})

	err := svc.RebuildAll(ctx)
	require.NoError(t, err)

	require.Equal(t, categorydomain.Category{ID: rootA, Name: "Root A", Slug: "root-a", Lft: 1, Rgt: 6, Depth: 0, SortOrder: 0}, repo.byID[rootA])
	require.Equal(t, categorydomain.Category{ID: childA, Name: "Alpha", Slug: "alpha", ParentID: &rootA, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0}, repo.byID[childA])
	require.Equal(t, categorydomain.Category{ID: childB, Name: "Beta", Slug: "beta", ParentID: &rootA, Lft: 4, Rgt: 5, Depth: 1, SortOrder: 1}, repo.byID[childB])
	require.Equal(t, categorydomain.Category{ID: rootB, Name: "Root B", Slug: "root-b", Lft: 7, Rgt: 8, Depth: 0, SortOrder: 1}, repo.byID[rootB])
}
