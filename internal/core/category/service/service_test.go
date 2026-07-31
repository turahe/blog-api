package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fixedIDs struct{ next uuid.UUID }

func (f fixedIDs) New() uuid.UUID { return f.next }

type fakeRepo struct {
	byID     map[uuid.UUID]categorydomain.Category
	bySlug   map[string]categorydomain.Category
	deleted  []uuid.UUID
	reorders []struct {
		parent *uuid.UUID
		ids    []uuid.UUID
	}
	maxSort map[string]int
}

func parentKey(parentID *uuid.UUID) string {
	if parentID == nil {
		return ""
	}
	return parentID.String()
}

func newFakeRepo(cats ...categorydomain.Category) *fakeRepo {
	byID := make(map[uuid.UUID]categorydomain.Category, len(cats))
	bySlug := make(map[string]categorydomain.Category, len(cats))
	for _, cat := range cats {
		byID[cat.ID] = cat
		bySlug[cat.Slug] = cat
	}
	return &fakeRepo{byID: byID, bySlug: bySlug, maxSort: map[string]int{}}
}

func (f *fakeRepo) List(context.Context) ([]categorydomain.Category, error) {
	out := make([]categorydomain.Category, 0, len(f.byID))
	for _, cat := range f.byID {
		out = append(out, cat)
	}
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
	f.byID[cat.ID] = cat
	f.bySlug[cat.Slug] = cat
	return cat, nil
}

func (f *fakeRepo) Update(_ context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	old, ok := f.byID[cat.ID]
	if ok && old.Slug != cat.Slug {
		delete(f.bySlug, old.Slug)
	}
	f.byID[cat.ID] = cat
	f.bySlug[cat.Slug] = cat
	return cat, nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	cat, ok := f.byID[id]
	if !ok {
		return categorydomain.ErrNotFound
	}
	delete(f.bySlug, cat.Slug)
	delete(f.byID, id)
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

func (f *fakeRepo) CountChildren(_ context.Context, parentID uuid.UUID) (int64, error) {
	var n int64
	for _, cat := range f.byID {
		if cat.ParentID != nil && *cat.ParentID == parentID {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) ListSiblingIDs(_ context.Context, parentID *uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, cat := range f.byID {
		if (parentID == nil && cat.ParentID == nil) ||
			(parentID != nil && cat.ParentID != nil && *cat.ParentID == *parentID) {
			out = append(out, cat.ID)
		}
	}
	return out, nil
}

func (f *fakeRepo) MaxSortOrder(_ context.Context, parentID *uuid.UUID) (int, error) {
	if v, ok := f.maxSort[parentKey(parentID)]; ok {
		return v, nil
	}
	max := -1
	for _, cat := range f.byID {
		same := (parentID == nil && cat.ParentID == nil) ||
			(parentID != nil && cat.ParentID != nil && *cat.ParentID == *parentID)
		if same && cat.SortOrder > max {
			max = cat.SortOrder
		}
	}
	return max, nil
}

func (f *fakeRepo) Reorder(_ context.Context, parentID *uuid.UUID, orderedIDs []uuid.UUID) error {
	f.reorders = append(f.reorders, struct {
		parent *uuid.UUID
		ids    []uuid.UUID
	}{parent: parentID, ids: append([]uuid.UUID{}, orderedIDs...)})
	for i, id := range orderedIDs {
		cat := f.byID[id]
		cat.SortOrder = i
		f.byID[id] = cat
	}
	return nil
}

func TestCreateSlugifiesNameAndAppendsSortOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	newID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := newFakeRepo()
	repo.maxSort[""] = 2
	svc := New(repo, fixedIDs{next: newID}, fixedClock{now: now})

	got, err := svc.Create(context.Background(), categorydomain.CreateInput{
		Name: "  Hello World  ",
	})
	require.NoError(t, err)
	require.Equal(t, "Hello World", got.Name)
	require.Equal(t, "hello-world", got.Slug)
	require.Equal(t, 3, got.SortOrder)
	require.Equal(t, newID, got.ID)
	require.Equal(t, now, got.CreatedAt)
}

func TestCreateRejectsEmptyNameAndInvalidSlug(t *testing.T) {
	t.Parallel()

	svc := New(newFakeRepo(), fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Create(context.Background(), categorydomain.CreateInput{Name: "   "})
	require.ErrorIs(t, err, ErrValidation)

	_, err = svc.Create(context.Background(), categorydomain.CreateInput{Name: "A", Slug: "Bad Slug"})
	require.ErrorIs(t, err, ErrValidation)
}

func TestCreateReturnsConflictWhenSlugTaken(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo(categorydomain.Category{
		ID: uuid.New(), Name: "Taken", Slug: "taken",
	})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Create(context.Background(), categorydomain.CreateInput{Name: "Other", Slug: "taken"})
	require.ErrorIs(t, err, ErrConflict)
}

func TestCreateRejectsMissingParent(t *testing.T) {
	t.Parallel()

	missing := uuid.New()
	svc := New(newFakeRepo(), fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Create(context.Background(), categorydomain.CreateInput{
		Name: "Child", ParentID: &missing,
	})
	require.ErrorIs(t, err, categorydomain.ErrNotFound)
}

func TestUpdateClearsParent(t *testing.T) {
	t.Parallel()

	rootID := uuid.New()
	childID := uuid.New()
	repo := newFakeRepo(
		categorydomain.Category{ID: rootID, Name: "Root", Slug: "root"},
		categorydomain.Category{ID: childID, Name: "Child", Slug: "child", ParentID: &rootID},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	got, err := svc.Update(context.Background(), childID, categorydomain.UpdateInput{
		ParentID: categorydomain.OptionalUUID{Present: true, Value: nil},
	})
	require.NoError(t, err)
	require.Nil(t, got.ParentID)
}

func TestUpdateRejectsCycle(t *testing.T) {
	t.Parallel()

	rootID := uuid.New()
	childID := uuid.New()
	repo := newFakeRepo(
		categorydomain.Category{ID: rootID, Name: "Root", Slug: "root"},
		categorydomain.Category{ID: childID, Name: "Child", Slug: "child", ParentID: &rootID},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Update(context.Background(), rootID, categorydomain.UpdateInput{
		ParentID: categorydomain.OptionalUUID{Present: true, Value: &childID},
	})
	require.ErrorIs(t, err, ErrValidation)
}

func TestUpdateRejectsSelfParent(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	repo := newFakeRepo(categorydomain.Category{ID: id, Name: "A", Slug: "a"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	_, err := svc.Update(context.Background(), id, categorydomain.UpdateInput{
		ParentID: categorydomain.OptionalUUID{Present: true, Value: &id},
	})
	require.ErrorIs(t, err, ErrValidation)
}

func TestDeleteRejectsWhenHasChildren(t *testing.T) {
	t.Parallel()

	parentID := uuid.New()
	childID := uuid.New()
	repo := newFakeRepo(
		categorydomain.Category{ID: parentID, Name: "Parent", Slug: "parent"},
		categorydomain.Category{ID: childID, Name: "Child", Slug: "child", ParentID: &parentID},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Delete(context.Background(), parentID)
	require.ErrorIs(t, err, categorydomain.ErrHasChildren)
	require.Empty(t, repo.deleted)
}

func TestDeleteSucceedsWhenLeaf(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	repo := newFakeRepo(categorydomain.Category{ID: id, Name: "Leaf", Slug: "leaf"})
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	require.NoError(t, svc.Delete(context.Background(), id))
	require.Equal(t, []uuid.UUID{id}, repo.deleted)
}

func TestReorderRequiresExactSiblingSet(t *testing.T) {
	t.Parallel()

	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	repo := newFakeRepo(
		categorydomain.Category{ID: a, Name: "A", Slug: "a", SortOrder: 0},
		categorydomain.Category{ID: b, Name: "B", Slug: "b", SortOrder: 1},
		categorydomain.Category{ID: c, Name: "C", Slug: "c", SortOrder: 0, ParentID: &a},
	)
	svc := New(repo, fixedIDs{next: uuid.New()}, fixedClock{now: time.Now()})

	err := svc.Reorder(context.Background(), nil, []uuid.UUID{b})
	require.ErrorIs(t, err, ErrValidation)

	err = svc.Reorder(context.Background(), nil, []uuid.UUID{b, a})
	require.NoError(t, err)
	require.Len(t, repo.reorders, 1)
	require.Equal(t, []uuid.UUID{b, a}, repo.reorders[0].ids)
}
