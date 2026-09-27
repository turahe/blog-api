package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
	categoryports "github.com/turahe/blog-api/internal/core/category/ports"
)

func newCategory(mutate func(*categorydomain.Category)) categorydomain.Category {
	slug := uniqueSlug("cat")
	now := time.Now().UTC().Truncate(time.Microsecond)

	cat := categorydomain.Category{UUID: uuid.New(), Name: slug, Slug: slug, CreatedAt: now, UpdatedAt: now}
	if mutate != nil {
		mutate(&cat)
	}

	return cat
}

func createCategory(t *testing.T, repo *CategoryRepository, mutate func(*categorydomain.Category)) categorydomain.Category {
	t.Helper()

	cat, err := repo.Create(t.Context(), newCategory(mutate))
	require.NoError(t, err)

	return cat
}

func TestCategoryRepositoryListOrdersByLeftBound(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	right := createCategory(t, repo, func(c *categorydomain.Category) { c.Lft, c.Rgt = 1_000_003, 1_000_004 })
	left := createCategory(t, repo, func(c *categorydomain.Category) { c.Lft, c.Rgt = 1_000_001, 1_000_002 })

	cats, err := repo.List(t.Context())
	require.NoError(t, err)

	var mine []uuid.UUID

	for _, cat := range cats {
		if cat.UUID == left.UUID || cat.UUID == right.UUID {
			mine = append(mine, cat.UUID)
		}
	}

	require.Equal(t, []uuid.UUID{left.UUID, right.UUID}, mine)

	_, err = repo.List(canceledContext(t))
	require.ErrorIs(t, err, context.Canceled)
}

func TestCategoryRepositoryGet(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	ctx := t.Context()
	parent := createCategory(t, repo, nil)
	image := insertReadyMedia(t, tx)
	cat := createCategory(t, repo, func(c *categorydomain.Category) {
		c.Description, c.ParentUUID, c.ImageUUID = "About Go", &parent.UUID, &image
		c.Lft, c.Rgt, c.Depth, c.SortOrder = 2, 3, 1, 4
	})
	require.NotZero(t, cat.ID)

	tests := []struct {
		name    string
		get     func() (categorydomain.Category, error)
		wantErr error
	}{
		{name: "by id", get: func() (categorydomain.Category, error) { return repo.GetByID(ctx, cat.UUID) }},
		{name: "by slug", get: func() (categorydomain.Category, error) { return repo.GetBySlug(ctx, cat.Slug) }},
		{name: "unknown id", get: func() (categorydomain.Category, error) { return repo.GetByID(ctx, uuid.New()) }, wantErr: categorydomain.ErrNotFound},
		{name: "unknown slug", get: func() (categorydomain.Category, error) {
			return repo.GetBySlug(ctx, uniqueSlug("none"))
		}, wantErr: categorydomain.ErrNotFound},
		{name: "id database error", get: func() (categorydomain.Category, error) {
			return repo.GetByID(canceledContext(t), cat.UUID)
		}, wantErr: context.Canceled},
		{name: "slug database error", get: func() (categorydomain.Category, error) {
			return repo.GetBySlug(canceledContext(t), cat.Slug)
		}, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.get()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, cat.UUID, got.UUID)
			require.Equal(t, "About Go", got.Description)
			require.Equal(t, &parent.UUID, got.ParentUUID)
			require.Equal(t, &image, got.ImageUUID)
			require.Equal(t, []int{2, 3, 1, 4}, []int{got.Lft, got.Rgt, got.Depth, got.SortOrder})
		})
	}

	got, err := repo.GetByID(ctx, parent.UUID)
	require.NoError(t, err)
	require.Empty(t, got.Description)
	require.Nil(t, got.ParentUUID)
	require.Nil(t, got.ImageUUID)
}

func TestCategoryRepositoryRejectsUnknownReferences(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	existing := createCategory(t, repo, nil)
	unknown := uuid.New()

	tests := []struct {
		name    string
		mutate  func(*categorydomain.Category)
		wantMsg string
	}{
		{name: "parent", mutate: func(c *categorydomain.Category) { c.ParentUUID = &unknown }, wantMsg: "parent_id not found"},
		{name: "image", mutate: func(c *categorydomain.Category) { c.ImageUUID = &unknown }, wantMsg: "image_id not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.Create(t.Context(), newCategory(tt.mutate))
			require.ErrorIs(t, err, categorydomain.ErrValidation)
			require.ErrorContains(t, err, tt.wantMsg)

			update := existing
			tt.mutate(&update)
			_, err = repo.Update(t.Context(), update)
			require.ErrorIs(t, err, categorydomain.ErrValidation)
			require.ErrorContains(t, err, tt.wantMsg)
		})
	}

	inSavepoint(t, tx, func() {
		_, err := repo.Create(t.Context(), newCategory(func(c *categorydomain.Category) { c.Slug = existing.Slug }))
		require.True(t, IsUniqueViolation(err), "slugs are unique: %v", err)
	})
}

func TestCategoryRepositoryUpdate(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	ctx := t.Context()
	parent, cat, other := createCategory(t, repo, nil), createCategory(t, repo, nil), createCategory(t, repo, nil)
	image := insertReadyMedia(t, tx)

	cat.Name, cat.Slug, cat.Description = "Renamed", uniqueSlug("renamed"), "Now described"
	cat.ParentUUID, cat.ImageUUID = &parent.UUID, &image
	cat.UpdatedAt = cat.UpdatedAt.Add(time.Minute)

	updated, err := repo.Update(ctx, cat)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, cat.Slug, updated.Slug)
	require.Equal(t, "Now described", updated.Description)
	require.Equal(t, &parent.UUID, updated.ParentUUID)
	require.Equal(t, &image, updated.ImageUUID)
	require.WithinDuration(t, cat.UpdatedAt, updated.UpdatedAt, 0)

	cat.Description, cat.ParentUUID, cat.ImageUUID = "", nil, nil
	updated, err = repo.Update(ctx, cat)
	require.NoError(t, err)
	require.Empty(t, updated.Description)
	require.Nil(t, updated.ParentUUID)
	require.Nil(t, updated.ImageUUID)

	_, err = repo.Update(ctx, newCategory(nil))
	require.ErrorIs(t, err, categorydomain.ErrNotFound)

	inSavepoint(t, tx, func() {
		cat.Slug = other.Slug
		_, err := repo.Update(ctx, cat)
		require.True(t, IsUniqueViolation(err), "slugs are unique: %v", err)
	})
}

func TestCategoryRepositoryDelete(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	cat := createCategory(t, repo, nil)

	require.NoError(t, repo.Delete(t.Context(), cat.UUID))

	_, err := repo.GetByID(t.Context(), cat.UUID)
	require.ErrorIs(t, err, categorydomain.ErrNotFound)
	require.ErrorIs(t, repo.Delete(t.Context(), cat.UUID), categorydomain.ErrNotFound)
	require.ErrorIs(t, repo.Delete(canceledContext(t), cat.UUID), context.Canceled)
}

func TestCategoryRepositoryCounts(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	ctx := t.Context()
	cat := createCategory(t, repo, nil)
	createCategory(t, repo, func(c *categorydomain.Category) { c.ParentUUID = &cat.UUID })
	createCategory(t, repo, func(c *categorydomain.Category) { c.ParentUUID = &cat.UUID })

	author := insertUser(t, tx)
	createPost(t, NewPostRepository(tx), postFixture{author: author, category: cat.UUID, createdAt: time.Now().UTC()})

	taken, err := repo.SlugTaken(ctx, cat.Slug, uuid.New())
	require.NoError(t, err)
	require.True(t, taken)

	taken, err = repo.SlugTaken(ctx, cat.Slug, cat.UUID)
	require.NoError(t, err)
	require.False(t, taken)

	posts, err := repo.CountPosts(ctx, cat.UUID)
	require.NoError(t, err)
	require.Equal(t, int64(1), posts)

	children, err := repo.CountChildren(ctx, cat.UUID)
	require.NoError(t, err)
	require.Equal(t, int64(2), children)
}

func TestCategoryRepositoryReplaceTreeBounds(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	ctx := t.Context()
	root, child := createCategory(t, repo, nil), createCategory(t, repo, nil)
	moved := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	root.Lft, root.Rgt, root.Depth, root.SortOrder, root.UpdatedAt = 1, 4, 0, 0, moved
	child.Lft, child.Rgt, child.Depth, child.SortOrder, child.ParentUUID, child.UpdatedAt = 2, 3, 1, 5, &root.UUID, moved

	require.NoError(t, repo.ReplaceTreeBounds(ctx, []categorydomain.Category{root, child}))

	gotRoot, err := repo.GetByID(ctx, root.UUID)
	require.NoError(t, err)
	require.Equal(t, []int{1, 4, 0, 0}, []int{gotRoot.Lft, gotRoot.Rgt, gotRoot.Depth, gotRoot.SortOrder})
	require.Nil(t, gotRoot.ParentUUID)
	require.WithinDuration(t, moved, gotRoot.UpdatedAt, 0)

	gotChild, err := repo.GetByID(ctx, child.UUID)
	require.NoError(t, err)
	require.Equal(t, []int{2, 3, 1, 5}, []int{gotChild.Lft, gotChild.Rgt, gotChild.Depth, gotChild.SortOrder})
	require.Equal(t, &root.UUID, gotChild.ParentUUID)

	require.ErrorIs(t, repo.ReplaceTreeBounds(canceledContext(t), []categorydomain.Category{root}), context.Canceled)
}

func TestCategoryRepositoryWithinTx(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewCategoryRepository(tx)
	ctx := t.Context()
	errAbort := errors.New("abort")
	kept, discarded := newCategory(nil), newCategory(nil)

	require.NoError(t, repo.WithinTx(ctx, func(ctx context.Context, inner categoryports.Repository) error {
		_, err := inner.Create(ctx, kept)
		return err
	}))

	err := repo.WithinTx(ctx, func(ctx context.Context, inner categoryports.Repository) error {
		_, err := inner.Create(ctx, discarded)
		require.NoError(t, err)

		return errAbort
	})
	require.ErrorIs(t, err, errAbort)

	_, err = repo.GetByID(ctx, kept.UUID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, discarded.UUID)
	require.ErrorIs(t, err, categorydomain.ErrNotFound, "a failed fn rolls back its writes")
}
