package persistence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

const repoTestKey = "repo-test-pagination-key-do-not-use-production-000000"

func TestMain(m *testing.M) {
	pagination.ResetGlobalSigner()
	os.Setenv("APP_KEY", repoTestKey)
	os.Exit(m.Run())
}

type postFixture struct {
	author, category uuid.UUID
	status           postdomain.Status
	title, slug      string
	createdAt        time.Time
	publishedAt      *time.Time
}

func createPost(t *testing.T, repo *PostRepository, f postFixture) postdomain.Post {
	t.Helper()

	if f.status == "" {
		f.status = postdomain.StatusDraft
	}

	if f.slug == "" {
		f.slug = uniqueSlug("post")
	}

	if f.title == "" {
		f.title = f.slug
	}

	var category *uuid.UUID
	if f.category != uuid.Nil {
		category = &f.category
	}

	post, err := repo.Create(context.Background(), postdomain.Post{
		UUID: uuid.New(), AuthorUUID: f.author, CategoryUUID: category,
		Title: f.title, Slug: f.slug, Status: f.status, Version: 1,
		PublishedAt: f.publishedAt, CreatedAt: f.createdAt, UpdatedAt: f.createdAt,
	})
	require.NoError(t, err)

	return post
}

func softDelete(t *testing.T, repo *PostRepository, post postdomain.Post) postdomain.Post {
	t.Helper()

	now := time.Now().UTC()
	post.DeletedAt = &now
	post.UpdatedAt = now
	post.Version++
	require.NoError(t, repo.SoftDelete(context.Background(), post))

	return post
}

func postIDs(items []postdomain.Post) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UUID)
	}

	return ids
}

func TestPostRepositoryListPublishedPaginatesStablyAndFilters(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostRepository(tx)
	ctx := context.Background()

	author := insertUser(t, tx)
	category := insertCategory(t, tx)
	other := insertCategory(t, tx)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	var published []uuid.UUID
	for range 5 {
		published = append(published, createPost(t, repo, postFixture{
			author: author, category: category, status: postdomain.StatusPublished, createdAt: at, publishedAt: &at,
		}).UUID)
	}

	createPost(t, repo, postFixture{author: author, category: category, createdAt: at})
	createPost(t, repo, postFixture{author: author, category: other, status: postdomain.StatusPublished, createdAt: at, publishedAt: &at})
	softDelete(t, repo, createPost(t, repo, postFixture{
		author: author, category: category, status: postdomain.StatusPublished, createdAt: at, publishedAt: &at,
	}))

	var seen []uuid.UUID

	for page := 1; page <= 3; page++ {
		result, err := repo.ListPublished(ctx, postdomain.ListFilter{
			PageRequest:  pagination.ParseLegacy(postListPublishedCfg, page, 2),
			CategoryUUID: &category,
		})
		require.NoError(t, err)
		require.NotNil(t, result.Total)
		require.Equal(t, int64(5), *result.Total)
		seen = append(seen, postIDs(result.Items)...)
	}

	require.ElementsMatch(t, published, seen, "identical timestamps must not repeat or skip rows across pages")

	tag := insertTag(t, tx, published[0], published[3])
	result, err := repo.ListPublished(ctx, postdomain.ListFilter{
		PageRequest: pagination.ParseLegacy(postListPublishedCfg, 1, 10),
		TagUUID:     &tag,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Total)
	require.Equal(t, int64(2), *result.Total)
	require.ElementsMatch(t, []uuid.UUID{published[0], published[3]}, postIDs(result.Items))
}

func TestPostRepositoryListAdminFilters(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostRepository(tx)
	ctx := context.Background()

	author := insertUser(t, tx)
	now := time.Now().UTC()
	alpha := createPost(t, repo, postFixture{author: author, title: "Alpha draft", createdAt: now.Add(-3 * time.Minute)})
	beta := createPost(t, repo, postFixture{author: author, title: "Beta story", status: postdomain.StatusPublished, createdAt: now.Add(-2 * time.Minute), publishedAt: &now})
	gamma := softDelete(t, repo, createPost(t, repo, postFixture{author: author, title: "Gamma gone", createdAt: now.Add(-time.Minute)}))

	list := func(filter postdomain.AdminListFilter) postdomain.ListResult {
		t.Helper()

		filter.AuthorUUID = &author
		filter.PageRequest = pagination.ParseLegacy(postListPublishedCfg, 1, 10)
		result, err := repo.ListAdmin(ctx, filter)
		require.NoError(t, err)

		return result
	}

	require.Equal(t, []uuid.UUID{beta.UUID, alpha.UUID}, postIDs(list(postdomain.AdminListFilter{}).Items), "newest first, deleted hidden")
	require.Equal(t, []uuid.UUID{alpha.UUID}, postIDs(list(postdomain.AdminListFilter{Status: "draft"}).Items))
	require.Equal(t, []uuid.UUID{beta.UUID}, postIDs(list(postdomain.AdminListFilter{Query: "BETA"}).Items))
	require.Equal(t, []uuid.UUID{beta.UUID}, postIDs(list(postdomain.AdminListFilter{Query: "a", Status: "published"}).Items),
		"the title/slug OR must stay grouped under the status filter")
	require.Equal(t, []uuid.UUID{gamma.UUID}, postIDs(list(postdomain.AdminListFilter{Trashed: true}).Items))

	page := list(postdomain.AdminListFilter{})
	require.NotNil(t, page.Total)
	require.Equal(t, int64(2), *page.Total)
}

func TestPostRepositoryGetPublishedBySlug(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostRepository(tx)
	author := insertUser(t, tx)
	now := time.Now().UTC()
	published := createPost(t, repo, postFixture{author: author, status: postdomain.StatusPublished, createdAt: now, publishedAt: &now})
	draft := createPost(t, repo, postFixture{author: author, createdAt: now})
	deleted := softDelete(t, repo, createPost(t, repo, postFixture{
		author: author, status: postdomain.StatusPublished, createdAt: now, publishedAt: &now,
	}))

	tests := []struct {
		name    string
		ctx     context.Context
		slug    string
		wantErr error
	}{
		{name: "published", ctx: t.Context(), slug: published.Slug},
		{name: "draft", ctx: t.Context(), slug: draft.Slug, wantErr: postdomain.ErrNotFound},
		{name: "deleted", ctx: t.Context(), slug: deleted.Slug, wantErr: postdomain.ErrNotFound},
		{name: "unknown", ctx: t.Context(), slug: uniqueSlug("missing"), wantErr: postdomain.ErrNotFound},
		{name: "database error", ctx: canceledContext(t), slug: published.Slug, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.GetPublishedBySlug(tt.ctx, tt.slug)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, published.UUID, got.UUID)
			require.Equal(t, postdomain.StatusPublished, got.Status)
		})
	}
}

func TestPostRepositorySoftDeleteRestoreAndSlugReuse(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostRepository(tx)
	ctx := context.Background()

	author := insertUser(t, tx)
	slug := uniqueSlug("reuse")
	original := createPost(t, repo, postFixture{author: author, slug: slug, status: postdomain.StatusPublished, createdAt: time.Now().UTC()})

	stale := original
	stale.Version += 5
	require.ErrorIs(t, repo.SoftDelete(ctx, stale), postdomain.ErrStaleVersion)

	deleted := softDelete(t, repo, original)
	_, err := repo.GetByID(ctx, original.UUID)
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	got, err := repo.GetDeletedByID(ctx, original.UUID)
	require.NoError(t, err)
	require.NotNil(t, got.DeletedAt)
	require.ErrorIs(t, repo.SoftDelete(ctx, softDeleteInput(deleted)), postdomain.ErrNotFound)

	replacement := createPost(t, repo, postFixture{author: author, slug: slug, createdAt: time.Now().UTC()})
	require.Equal(t, slug, replacement.Slug, "a deleted post releases its slug")

	createPost(t, repo, postFixture{author: author, slug: slug + "-tips", createdAt: time.Now().UTC()})
	createPost(t, repo, postFixture{author: author, slug: slug + "x", createdAt: time.Now().UTC()})
	slugs, err := repo.SlugsWithPrefix(ctx, slug)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{slug, slug + "-tips"}, slugs, "deleted and non-hyphenated prefixes excluded")

	restore := deleted
	restore.DeletedAt = nil
	restore.Status = postdomain.StatusDraft
	restore.PublishedAt = nil
	restore.Version++

	require.NoError(t, tx.SavePoint("restore_conflict").Error)

	_, err = repo.Restore(ctx, restore)
	require.ErrorIs(t, err, postdomain.ErrConflict)
	require.NotErrorIs(t, err, postdomain.ErrStaleVersion)
	require.NoError(t, tx.RollbackTo("restore_conflict").Error)

	restore.Slug = slug + "-2"
	restored, err := repo.Restore(ctx, restore)
	require.NoError(t, err)
	require.Nil(t, restored.DeletedAt)
	require.Equal(t, slug+"-2", restored.Slug)
	require.Equal(t, postdomain.StatusDraft, restored.Status)
	require.Equal(t, restore.Version, restored.Version)

	_, err = repo.Restore(ctx, restore)
	require.ErrorIs(t, err, postdomain.ErrNotFound, "a live post cannot be restored")
}

func TestPostRepositoryUpdateMapsSlugConflict(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewPostRepository(tx)
	author := insertUser(t, tx)
	taken := createPost(t, repo, postFixture{author: author, createdAt: time.Now().UTC()})
	post := createPost(t, repo, postFixture{author: author, createdAt: time.Now().UTC()})

	post.Slug = taken.Slug
	post.Version++

	_, err := repo.Update(context.Background(), post)
	require.ErrorIs(t, err, postdomain.ErrConflict)
	require.NotErrorIs(t, err, postdomain.ErrStaleVersion)
}

func softDeleteInput(post postdomain.Post) postdomain.Post {
	post.Version++
	return post
}
