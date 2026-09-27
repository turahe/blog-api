package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

func createTag(t *testing.T, repo *TagRepository, name string) tagdomain.Tag {
	t.Helper()

	tag, err := repo.Create(t.Context(), tagdomain.Tag{
		UUID: uuid.New(), Name: name, Slug: uniqueSlug("tag"), CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	return tag
}

func requireSameTag(t *testing.T, want, got tagdomain.Tag) {
	t.Helper()

	require.Equal(t, want.UUID, got.UUID)
	require.Equal(t, want.Name, got.Name)
	require.Equal(t, want.Slug, got.Slug)
	require.WithinDuration(t, want.CreatedAt, got.CreatedAt, time.Microsecond)
}

func tagIDs(tags []tagdomain.Tag) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tag.UUID)
	}

	return out
}

func TestTagRepositoryListOrdersByName(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	prefix := uniqueSlug("list")
	beta, alpha := createTag(t, repo, prefix+" beta"), createTag(t, repo, prefix+" alpha")

	tags, err := repo.List(t.Context())
	require.NoError(t, err)

	var mine []uuid.UUID

	for _, tag := range tags {
		if tag.UUID == alpha.UUID || tag.UUID == beta.UUID {
			mine = append(mine, tag.UUID)
		}
	}

	require.Equal(t, []uuid.UUID{alpha.UUID, beta.UUID}, mine)

	_, err = repo.List(canceledContext(t))
	require.ErrorIs(t, err, context.Canceled)
}

func TestTagRepositoryGet(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	ctx := t.Context()
	tag := createTag(t, repo, "Go")

	tests := []struct {
		name    string
		get     func() (tagdomain.Tag, error)
		wantErr error
	}{
		{name: "by id", get: func() (tagdomain.Tag, error) { return repo.GetByID(ctx, tag.UUID) }},
		{name: "by slug", get: func() (tagdomain.Tag, error) { return repo.GetBySlug(ctx, tag.Slug) }},
		{name: "unknown id", get: func() (tagdomain.Tag, error) { return repo.GetByID(ctx, uuid.New()) }, wantErr: tagdomain.ErrNotFound},
		{name: "unknown slug", get: func() (tagdomain.Tag, error) { return repo.GetBySlug(ctx, uniqueSlug("none")) }, wantErr: tagdomain.ErrNotFound},
		{name: "id database error", get: func() (tagdomain.Tag, error) {
			return repo.GetByID(canceledContext(t), tag.UUID)
		}, wantErr: context.Canceled},
		{name: "slug database error", get: func() (tagdomain.Tag, error) {
			return repo.GetBySlug(canceledContext(t), tag.Slug)
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
			requireSameTag(t, tag, got)
		})
	}
}

func TestTagRepositoryCreateRejectsDuplicateSlug(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	tag := createTag(t, repo, "Go")

	inSavepoint(t, tx, func() {
		_, err := repo.Create(t.Context(), tagdomain.Tag{UUID: uuid.New(), Name: "Go", Slug: tag.Slug, CreatedAt: time.Now().UTC()})
		require.True(t, IsUniqueViolation(err), "slugs are unique: %v", err)
	})
}

func TestTagRepositoryUpdate(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	tag, other := createTag(t, repo, "Go"), createTag(t, repo, "Rust")

	tag.Name, tag.Slug = "Golang", uniqueSlug("golang")
	updated, err := repo.Update(t.Context(), tag)
	require.NoError(t, err)
	requireSameTag(t, tag, updated)

	_, err = repo.Update(t.Context(), tagdomain.Tag{UUID: uuid.New(), Name: "x", Slug: uniqueSlug("x")})
	require.ErrorIs(t, err, tagdomain.ErrNotFound)

	inSavepoint(t, tx, func() {
		tag.Slug = other.Slug
		_, err := repo.Update(t.Context(), tag)
		require.True(t, IsUniqueViolation(err), "slugs are unique: %v", err)
	})
}

func TestTagRepositorySlugTaken(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	tag := createTag(t, repo, "Go")

	tests := []struct {
		name    string
		slug    string
		exclude uuid.UUID
		want    bool
	}{
		{name: "used by another tag", slug: tag.Slug, exclude: uuid.New(), want: true},
		{name: "used only by the excluded tag", slug: tag.Slug, exclude: tag.UUID},
		{name: "unused", slug: uniqueSlug("free"), exclude: uuid.Nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taken, err := repo.SlugTaken(t.Context(), tt.slug, tt.exclude)
			require.NoError(t, err)
			require.Equal(t, tt.want, taken)
		})
	}
}

func TestTagRepositoryMergeInto(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	ctx := t.Context()
	author := insertUser(t, tx)
	posts := NewPostRepository(tx)

	post := func() uuid.UUID {
		return createPost(t, posts, postFixture{author: author, createdAt: time.Now().UTC()}).UUID
	}
	p1, p2, p3 := post(), post(), post()
	source, target := insertTag(t, tx, p1, p2), insertTag(t, tx, p2, p3)

	count, err := repo.CountPosts(ctx, source)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	require.NoError(t, repo.MergeInto(ctx, source, target))

	count, err = repo.CountPosts(ctx, target)
	require.NoError(t, err)
	require.Equal(t, int64(3), count, "shared posts are linked once")

	_, err = repo.GetByID(ctx, source)
	require.ErrorIs(t, err, tagdomain.ErrNotFound)

	count, err = repo.CountPosts(ctx, source)
	require.NoError(t, err)
	require.Zero(t, count)

	require.ErrorIs(t, repo.MergeInto(ctx, uuid.New(), target), tagdomain.ErrNotFound)
	require.ErrorIs(t, repo.MergeInto(ctx, target, uuid.New()), errUnknownReference)

	_, err = repo.GetByID(ctx, target)
	require.NoError(t, err, "a failed merge rolls back")

	require.ErrorIs(t, repo.MergeInto(canceledContext(t), target, target), context.Canceled)
}

func TestTagRepositoryDelete(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	tag := createTag(t, repo, "Go")

	require.NoError(t, repo.Delete(t.Context(), tag.UUID))

	_, err := repo.GetByID(t.Context(), tag.UUID)
	require.ErrorIs(t, err, tagdomain.ErrNotFound)
	require.ErrorIs(t, repo.Delete(t.Context(), tag.UUID), tagdomain.ErrNotFound)
	require.ErrorIs(t, repo.Delete(canceledContext(t), tag.UUID), context.Canceled)
}

func TestTagRepositoryReplacePostTags(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewTagRepository(tx)
	ctx := t.Context()
	post := createPost(t, NewPostRepository(tx), postFixture{author: insertUser(t, tx), createdAt: time.Now().UTC()}).UUID
	prefix := uniqueSlug("replace")
	zeta, alpha, beta := createTag(t, repo, prefix+" zeta"), createTag(t, repo, prefix+" alpha"), createTag(t, repo, prefix+" beta")

	listed := func() []uuid.UUID {
		t.Helper()

		tags, err := repo.ListByPostID(ctx, post)
		require.NoError(t, err)

		return tagIDs(tags)
	}

	require.NoError(t, repo.ReplacePostTags(ctx, post, []uuid.UUID{zeta.UUID, alpha.UUID, zeta.UUID}))
	require.Equal(t, []uuid.UUID{alpha.UUID, zeta.UUID}, listed(), "duplicates collapse; listed by name")

	require.NoError(t, repo.ReplacePostTags(ctx, post, []uuid.UUID{beta.UUID}))
	require.Equal(t, []uuid.UUID{beta.UUID}, listed())

	require.ErrorIs(t, repo.ReplacePostTags(ctx, post, []uuid.UUID{alpha.UUID, uuid.New()}), errUnknownReference)
	require.Equal(t, []uuid.UUID{beta.UUID}, listed(), "an unknown tag rolls back the replacement")

	require.ErrorIs(t, repo.ReplacePostTags(ctx, uuid.New(), nil), errUnknownReference)

	require.NoError(t, repo.ReplacePostTags(ctx, post, nil))
	require.Empty(t, listed())

	_, err := repo.ListByPostID(canceledContext(t), post)
	require.ErrorIs(t, err, context.Canceled)
}
