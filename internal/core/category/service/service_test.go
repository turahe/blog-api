package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
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

	// listHook, when set, rewrites what List returns, simulating concurrent writers.
	listHook         func([]categorydomain.Category) []categorydomain.Category
	listErr          error
	getErr           error
	createErr        error
	updateErr        error
	deleteErr        error
	slugErr          error
	countChildrenErr error
	countPostsErr    error
	boundsErr        error
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
	f.byID[cat.UUID] = cat
	f.bySlug[cat.Slug] = cat
}

func (f *fakeRepo) List(context.Context) ([]categorydomain.Category, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

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

	if f.listHook != nil {
		out = f.listHook(out)
	}

	return out, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (categorydomain.Category, error) {
	if f.getErr != nil {
		return categorydomain.Category{}, f.getErr
	}

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
	if f.createErr != nil {
		return categorydomain.Category{}, f.createErr
	}

	f.put(cat)
	return cat, nil
}

func (f *fakeRepo) Update(_ context.Context, cat categorydomain.Category) (categorydomain.Category, error) {
	if f.updateErr != nil {
		return categorydomain.Category{}, f.updateErr
	}

	old, ok := f.byID[cat.UUID]
	if ok && old.Slug != cat.Slug {
		delete(f.bySlug, old.Slug)
	}

	f.put(cat)

	return cat, nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

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
	if f.slugErr != nil {
		return false, f.slugErr
	}

	cat, ok := f.bySlug[slug]
	if !ok {
		return false, nil
	}

	return cat.UUID != excludeID, nil
}

func (f *fakeRepo) CountPosts(_ context.Context, categoryID uuid.UUID) (int64, error) {
	return f.posts[categoryID], f.countPostsErr
}

func (f *fakeRepo) CountChildren(_ context.Context, categoryID uuid.UUID) (int64, error) {
	if f.countChildrenErr != nil {
		return 0, f.countChildrenErr
	}

	var n int64

	for _, cat := range f.byID {
		if cat.ParentUUID != nil && *cat.ParentUUID == categoryID {
			n++
		}
	}

	return n, nil
}

func (f *fakeRepo) ReplaceTreeBounds(_ context.Context, cats []categorydomain.Category) error {
	if f.boundsErr != nil {
		return f.boundsErr
	}

	f.rebuilds++
	for _, cat := range cats {
		old, ok := f.byID[cat.UUID]
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
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	newID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ids := &fixedIDs{next: []uuid.UUID{newID}}
	repo := newFakeRepo()
	svc := New(repo, ids, fixedClock{now: now})

	got, err := svc.Create(ctx, CreateInput{Name: "  Hello World  "})
	require.NoError(t, err)
	require.Equal(t, newID, got.UUID)
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
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	parentID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	firstID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	secondID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	newID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{UUID: parentID, Name: "Parent", Slug: "parent", Lft: 1, Rgt: 6, SortOrder: 0},
		categorydomain.Category{UUID: firstID, Name: "Apple", Slug: "apple", ParentUUID: &parentID, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0},
		categorydomain.Category{UUID: secondID, Name: "Banana", Slug: "banana", ParentUUID: &parentID, Lft: 4, Rgt: 5, Depth: 1, SortOrder: 1},
	)
	svc := New(repo, &fixedIDs{next: []uuid.UUID{newID}}, fixedClock{now: now})

	got, err := svc.Create(ctx, CreateInput{Name: "Cherry", ParentID: &parentID, BeforeID: &secondID})
	require.NoError(t, err)
	require.Equal(t, newID, got.UUID)
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
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	catID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	otherID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	parentID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	imageID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{UUID: catID, Name: "Mine", Slug: "mine", Description: "old", ParentUUID: &parentID, ImageUUID: &imageID, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0},
		categorydomain.Category{UUID: otherID, Name: "Other", Slug: "other"},
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
	require.Empty(t, got.Description)
	require.Nil(t, got.ImageUUID)
	require.Equal(t, &parentID, got.ParentUUID)
	require.Equal(t, 2, got.Lft)
	require.Equal(t, 3, got.Rgt)
	require.Equal(t, now, got.UpdatedAt)
	require.Zero(t, repo.rebuilds)
}

func TestMoveRejectsOwnDescendantAndReparentsSuccessfully(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	childID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	grandID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	otherRootID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(rebuildBounds([]categorydomain.Category{
		{UUID: rootID, Name: "Root", Slug: "root", SortOrder: 0},
		{UUID: childID, Name: "Child", Slug: "child", ParentUUID: &rootID, SortOrder: 0},
		{UUID: grandID, Name: "Grand", Slug: "grand", ParentUUID: &childID, SortOrder: 0},
		{UUID: otherRootID, Name: "Other", Slug: "other", SortOrder: 1},
	})...)
	svc := New(repo, &fixedIDs{}, fixedClock{now: time.Date(2026, 8, 1, 4, 0, 0, 0, time.UTC)})

	_, err := svc.Move(ctx, childID, &grandID, nil)
	require.ErrorIs(t, err, ErrValidation)

	got, err := svc.Move(ctx, childID, &otherRootID, nil)
	require.NoError(t, err)
	require.Equal(t, &otherRootID, got.ParentUUID)
	require.Equal(t, 1, got.Depth)
	require.Equal(t, 2, repo.byID[grandID].Depth)
	require.Less(t, repo.byID[otherRootID].Lft, got.Lft)
	require.Less(t, got.Lft, repo.byID[grandID].Lft)
}

func TestDeleteBlocksInUseAndRebuildsAfterDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	childID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	usedID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	freeID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(rebuildBounds([]categorydomain.Category{
		{UUID: rootID, Name: "Root", Slug: "root"},
		{UUID: childID, Name: "Child", Slug: "child", ParentUUID: &rootID},
		{UUID: usedID, Name: "Used", Slug: "used", SortOrder: 1},
		{UUID: freeID, Name: "Free", Slug: "free", SortOrder: 2},
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
	t.Parallel()

	ctx := context.Background()
	rootA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	rootB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	childB := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	childA := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	repo := newFakeRepo(
		categorydomain.Category{UUID: childA, Name: "Alpha", Slug: "alpha", ParentUUID: &rootA, SortOrder: 10},
		categorydomain.Category{UUID: rootB, Name: "Root B", Slug: "root-b", SortOrder: 1},
		categorydomain.Category{UUID: rootA, Name: "Root A", Slug: "root-a", SortOrder: 0},
		categorydomain.Category{UUID: childB, Name: "Beta", Slug: "beta", ParentUUID: &rootA, SortOrder: 10},
	)
	svc := New(repo, &fixedIDs{}, fixedClock{now: time.Now()})

	err := svc.RebuildAll(ctx)
	require.NoError(t, err)

	require.Equal(t, categorydomain.Category{UUID: rootA, Name: "Root A", Slug: "root-a", Lft: 1, Rgt: 6, Depth: 0, SortOrder: 0}, repo.byID[rootA])
	require.Equal(t, categorydomain.Category{UUID: childA, Name: "Alpha", Slug: "alpha", ParentUUID: &rootA, Lft: 2, Rgt: 3, Depth: 1, SortOrder: 0}, repo.byID[childA])
	require.Equal(t, categorydomain.Category{UUID: childB, Name: "Beta", Slug: "beta", ParentUUID: &rootA, Lft: 4, Rgt: 5, Depth: 1, SortOrder: 1}, repo.byID[childB])
	require.Equal(t, categorydomain.Category{UUID: rootB, Name: "Root B", Slug: "root-b", Lft: 7, Rgt: 8, Depth: 0, SortOrder: 1}, repo.byID[rootB])
}

func TestGetBySlugBlankIsNotFound(t *testing.T) {
	t.Parallel()

	svc := New(newFakeRepo(), &fixedIDs{}, fixedClock{})

	_, err := svc.GetBySlug(t.Context(), "   ")

	require.ErrorIs(t, err, categorydomain.ErrNotFound)
}

// categoryTree seeds a root with one child and a second root.
type categoryTree struct {
	repo                     *fakeRepo
	parentID, childID, other uuid.UUID
}

func newCategoryTree() categoryTree {
	tree := categoryTree{
		parentID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		childID:  uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		other:    uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
	}
	tree.repo = newFakeRepo(rebuildBounds([]categorydomain.Category{
		{UUID: tree.parentID, Name: "Parent", Slug: "parent"},
		{UUID: tree.childID, Name: "Child", Slug: "child", ParentUUID: &tree.parentID},
		{UUID: tree.other, Name: "Other", Slug: "other", SortOrder: 1},
	})...)

	return tree
}

func without(id uuid.UUID) func([]categorydomain.Category) []categorydomain.Category {
	return func(cats []categorydomain.Category) []categorydomain.Category {
		return slices.DeleteFunc(cats, func(c categorydomain.Category) bool { return c.UUID == id })
	}
}

func reparented(id uuid.UUID, parent *uuid.UUID) func([]categorydomain.Category) []categorydomain.Category {
	return func(cats []categorydomain.Category) []categorydomain.Category {
		for i := range cats {
			if cats[i].UUID == id {
				cats[i].ParentUUID = parent
			}
		}

		return cats
	}
}

func TestCreateFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("database down")

	tests := []struct {
		name  string
		in    func(categoryTree) CreateInput
		setup func(categoryTree)
		want  error
	}{
		{
			name: "name too long",
			in:   func(categoryTree) CreateInput { return CreateInput{Name: strings.Repeat("é", 129)} },
			want: ErrValidation,
		},
		{
			name: "name without slug characters",
			in:   func(categoryTree) CreateInput { return CreateInput{Name: "!!!"} },
			want: ErrValidation,
		},
		{
			name: "invalid explicit slug",
			in:   func(categoryTree) CreateInput { return CreateInput{Name: "News", Slug: "not a slug"} },
			want: ErrValidation,
		},
		{
			name:  "slug lookup fails",
			in:    func(categoryTree) CreateInput { return CreateInput{Name: "News"} },
			setup: func(tree categoryTree) { tree.repo.slugErr = failure },
			want:  failure,
		},
		{
			name: "parent not found",
			in:   func(categoryTree) CreateInput { return CreateInput{Name: "News", ParentID: new(uuid.New())} },
			want: ErrValidation,
		},
		{
			name:  "parent lookup fails",
			in:    func(tree categoryTree) CreateInput { return CreateInput{Name: "News", ParentID: &tree.parentID} },
			setup: func(tree categoryTree) { tree.repo.getErr = failure },
			want:  failure,
		},
		{
			name: "before not found",
			in:   func(categoryTree) CreateInput { return CreateInput{Name: "News", BeforeID: new(uuid.New())} },
			want: ErrValidation,
		},
		{
			name:  "before lookup fails",
			in:    func(tree categoryTree) CreateInput { return CreateInput{Name: "News", BeforeID: &tree.other} },
			setup: func(tree categoryTree) { tree.repo.getErr = failure },
			want:  failure,
		},
		{
			name: "before under another parent",
			in:   func(tree categoryTree) CreateInput { return CreateInput{Name: "News", BeforeID: &tree.childID} },
			want: ErrValidation,
		},
		{
			name:  "list fails",
			in:    func(categoryTree) CreateInput { return CreateInput{Name: "News"} },
			setup: func(tree categoryTree) { tree.repo.listErr = failure },
			want:  failure,
		},
		{
			name:  "insert fails",
			in:    func(categoryTree) CreateInput { return CreateInput{Name: "News"} },
			setup: func(tree categoryTree) { tree.repo.createErr = failure },
			want:  failure,
		},
		{
			name:  "before deleted concurrently",
			in:    func(tree categoryTree) CreateInput { return CreateInput{Name: "News", BeforeID: &tree.other} },
			setup: func(tree categoryTree) { tree.repo.listHook = without(tree.other) },
			want:  ErrValidation,
		},
		{
			name:  "before moved concurrently",
			in:    func(tree categoryTree) CreateInput { return CreateInput{Name: "News", BeforeID: &tree.other} },
			setup: func(tree categoryTree) { tree.repo.listHook = reparented(tree.other, &tree.parentID) },
			want:  ErrValidation,
		},
		{
			name:  "bounds write fails",
			in:    func(categoryTree) CreateInput { return CreateInput{Name: "News"} },
			setup: func(tree categoryTree) { tree.repo.boundsErr = failure },
			want:  failure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := newCategoryTree()
			if tc.setup != nil {
				tc.setup(tree)
			}

			_, err := New(tree.repo, &fixedIDs{}, fixedClock{}).Create(t.Context(), tc.in(tree))

			require.ErrorIs(t, err, tc.want)
			require.Zero(t, tree.repo.rebuilds)
		})
	}
}

func TestCreateOrdersTiedSiblingsByNameThenID(t *testing.T) {
	t.Parallel()

	sameA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	sameB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	alpha := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	beta := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	newID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	repo := newFakeRepo(
		categorydomain.Category{UUID: sameB, Name: "Same", Slug: "same-b"},
		categorydomain.Category{UUID: beta, Name: "Beta", Slug: "beta"},
		categorydomain.Category{UUID: sameA, Name: "Same", Slug: "same-a"},
		categorydomain.Category{UUID: alpha, Name: "Alpha", Slug: "alpha"},
	)
	svc := New(repo, &fixedIDs{next: []uuid.UUID{newID}}, fixedClock{})

	got, err := svc.Create(t.Context(), CreateInput{Name: "Zulu", Description: new("Last one")})
	require.NoError(t, err)
	require.Equal(t, "Last one", got.Description)

	order := map[uuid.UUID]int{}
	for id, cat := range repo.byID {
		order[id] = cat.SortOrder
	}

	require.Equal(t, map[uuid.UUID]int{alpha: 0, beta: 1, sameA: 2, sameB: 3, newID: 4}, order)
}

func TestUpdateFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("database down")

	tests := []struct {
		name  string
		in    UpdateInput
		setup func(*fakeRepo)
		want  error
	}{
		{name: "no fields", in: UpdateInput{}, want: ErrValidation},
		{name: "blank name", in: UpdateInput{Name: new("  ")}, want: ErrValidation},
		{name: "blank slug", in: UpdateInput{Slug: new("  ")}, want: ErrValidation},
		{name: "invalid slug", in: UpdateInput{Slug: new("Not Valid")}, want: ErrValidation},
		{name: "slug lookup fails", in: UpdateInput{Slug: new("fresh")}, setup: func(r *fakeRepo) { r.slugErr = failure }, want: failure},
		{name: "store fails", in: UpdateInput{Name: new("New")}, setup: func(r *fakeRepo) { r.updateErr = failure }, want: failure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := newCategoryTree()
			if tc.setup != nil {
				tc.setup(tree.repo)
			}

			_, err := New(tree.repo, &fixedIDs{}, fixedClock{}).Update(t.Context(), tree.childID, tc.in)

			require.ErrorIs(t, err, tc.want)
			require.Equal(t, "Child", tree.repo.byID[tree.childID].Name)
			require.Equal(t, "child", tree.repo.byID[tree.childID].Slug)
		})
	}
}

func TestUpdateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		slug    string
		slugErr error
		want    string
	}{
		{name: "unchanged slug needs no lookup", slug: " CHILD ", slugErr: errors.New("must not be called"), want: "child"},
		{name: "free slug is taken over", slug: "Fresh-Slug", want: "fresh-slug"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := newCategoryTree()
			tree.repo.slugErr = tc.slugErr

			got, err := New(tree.repo, &fixedIDs{}, fixedClock{}).Update(t.Context(), tree.childID, UpdateInput{Slug: &tc.slug})

			require.NoError(t, err)
			require.Equal(t, tc.want, got.Slug)
			require.Equal(t, tc.want, tree.repo.byID[tree.childID].Slug)
		})
	}
}

func TestDeleteFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("database down")

	tests := []struct {
		name  string
		setup func(*fakeRepo)
	}{
		{name: "child count fails", setup: func(r *fakeRepo) { r.countChildrenErr = failure }},
		{name: "post count fails", setup: func(r *fakeRepo) { r.countPostsErr = failure }},
		{name: "delete fails", setup: func(r *fakeRepo) { r.deleteErr = failure }},
		{name: "list fails", setup: func(r *fakeRepo) { r.listErr = failure }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := newCategoryTree()
			tc.setup(tree.repo)

			require.ErrorIs(t, New(tree.repo, &fixedIDs{}, fixedClock{}).Delete(t.Context(), tree.other), failure)
			require.Zero(t, tree.repo.rebuilds)
		})
	}
}

func TestMoveFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("database down")

	tests := []struct {
		name     string
		id       func(categoryTree) uuid.UUID
		parentID func(categoryTree) *uuid.UUID
		beforeID func(categoryTree) *uuid.UUID
		setup    func(*fakeRepo)
		want     error
	}{
		{
			name:     "under itself",
			id:       func(tree categoryTree) uuid.UUID { return tree.other },
			parentID: func(tree categoryTree) *uuid.UUID { return &tree.other },
			want:     ErrValidation,
		},
		{
			name:     "parent not found",
			id:       func(tree categoryTree) uuid.UUID { return tree.other },
			parentID: func(categoryTree) *uuid.UUID { return new(uuid.New()) },
			want:     ErrValidation,
		},
		{
			name:  "list fails",
			id:    func(tree categoryTree) uuid.UUID { return tree.other },
			setup: func(r *fakeRepo) { r.listErr = failure },
			want:  failure,
		},
		{
			name: "category not found",
			id:   func(categoryTree) uuid.UUID { return uuid.New() },
			want: categorydomain.ErrNotFound,
		},
		{
			name:     "before itself",
			id:       func(tree categoryTree) uuid.UUID { return tree.other },
			beforeID: func(tree categoryTree) *uuid.UUID { return &tree.other },
			want:     ErrValidation,
		},
		{
			name:  "store fails",
			id:    func(tree categoryTree) uuid.UUID { return tree.other },
			setup: func(r *fakeRepo) { r.updateErr = failure },
			want:  failure,
		},
		{
			name:  "bounds write fails",
			id:    func(tree categoryTree) uuid.UUID { return tree.other },
			setup: func(r *fakeRepo) { r.boundsErr = failure },
			want:  failure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree := newCategoryTree()
			if tc.setup != nil {
				tc.setup(tree.repo)
			}

			var parentID, beforeID *uuid.UUID
			if tc.parentID != nil {
				parentID = tc.parentID(tree)
			}

			if tc.beforeID != nil {
				beforeID = tc.beforeID(tree)
			}

			_, err := New(tree.repo, &fixedIDs{}, fixedClock{}).Move(t.Context(), tc.id(tree), parentID, beforeID)

			require.ErrorIs(t, err, tc.want)
			require.Nil(t, tree.repo.byID[tree.other].ParentUUID)
			require.Zero(t, tree.repo.rebuilds)
		})
	}
}

func TestRebuildAll(t *testing.T) {
	t.Parallel()

	failure := errors.New("database down")

	tests := []struct {
		name         string
		listErr      error
		wantRebuilds int
	}{
		{name: "empty tree writes no bounds", wantRebuilds: 1},
		{name: "list fails", listErr: failure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeRepo()
			repo.listErr = tc.listErr

			err := New(repo, &fixedIDs{}, fixedClock{}).RebuildAll(t.Context())

			require.ErrorIs(t, err, tc.listErr)
			require.Equal(t, tc.wantRebuilds, repo.rebuilds)
			require.Empty(t, repo.byID)
		})
	}
}

func TestRebuildBoundsBreaksTiesByNameThenID(t *testing.T) {
	t.Parallel()

	sameA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	sameB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	alpha := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	got := rebuildBounds([]categorydomain.Category{
		{UUID: sameB, Name: "Same"},
		{UUID: alpha, Name: "Alpha"},
		{UUID: sameA, Name: "Same"},
	})

	ids := make([]uuid.UUID, 0, len(got))
	for _, cat := range got {
		ids = append(ids, cat.UUID)
	}

	require.Equal(t, []uuid.UUID{alpha, sameA, sameB}, ids)
	require.Equal(t, []int{1, 3, 5}, []int{got[0].Lft, got[1].Lft, got[2].Lft})
}

func TestCreateDerivesSlugFromName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "Tips & Tricks", want: "tips-tricks"},
		{name: "  under_score  name ", want: "under-score-name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(newFakeRepo(), &fixedIDs{}, fixedClock{}).Create(t.Context(), CreateInput{Name: tc.name})

			require.NoError(t, err)
			require.Equal(t, tc.want, got.Slug)
		})
	}
}
