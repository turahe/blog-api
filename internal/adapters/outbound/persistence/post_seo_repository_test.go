package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type fixedSEODefaults struct{}

func (fixedSEODefaults) SEODefaults(context.Context) (postdomain.SEODefaults, error) {
	return postdomain.SEODefaults{SiteName: "Blog", TitleTemplate: "{title} | {site}"}, nil
}

func TestPostSEORepositoryRoundTripAndUpsert(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	posts, _ := revisionPosts(tx)
	repo := NewPostSEORepository(tx)
	author := insertUser(t, tx)
	image := insertReadyMedia(t, tx)

	post, _, err := posts.CreateDraft(ctx, author, "SEO post", "", "", "body", nil, nil)
	require.NoError(t, err)

	empty, err := repo.Get(ctx, post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.SEO{}, empty)

	seo := postdomain.SEO{
		Title: "Title", Description: "Description", Keywords: []string{"go", "sql"},
		OGImageUUID: &image, TwitterCard: postdomain.TwitterSummary, TwitterImageUUID: &image,
		TwitterCreator: "@blog", CanonicalURL: "https://blog.example.com/x", RobotsNoindex: true,
	}
	require.NoError(t, repo.Save(ctx, post.UUID, seo, time.Now()))

	got, err := repo.Get(ctx, post.UUID)
	require.NoError(t, err)
	require.Equal(t, seo, got)

	require.NoError(t, repo.Save(ctx, post.UUID, postdomain.SEO{Title: "Only title"}, time.Now()))

	got, err = repo.Get(ctx, post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.SEO{Title: "Only title"}, got)

	missing := uuid.New()
	require.ErrorIs(t, repo.Save(ctx, post.UUID, postdomain.SEO{OGImageUUID: &missing}, time.Now()), postdomain.ErrValidation)
	require.ErrorIs(t, repo.Save(ctx, uuid.New(), postdomain.SEO{}, time.Now()), postdomain.ErrNotFound)
}

func TestPostSEOUpdateSnapshotsIntoRevisions(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	posts, revs := revisionPosts(tx)
	posts.WithSEO(NewPostSEORepository(tx), fixedSEODefaults{}, nil)
	author := insertUser(t, tx)

	post, _, err := posts.CreateDraft(ctx, author, "Snapshot post", "", "", "body", nil, nil)
	require.NoError(t, err)

	title := "Search title"
	newSlug := "seo-renamed-" + uuid.NewString()[:8]
	view, err := posts.UpdateSEO(ctx, post.UUID, author, false, true, postdomain.SEOPatch{Title: &title, Slug: &newSlug})
	require.NoError(t, err)
	require.Equal(t, newSlug, view.Slug)

	latest, err := revs.Latest(ctx, post.UUID)
	require.NoError(t, err)
	require.Equal(t, postdomain.RevisionUpdate, latest.Type)
	require.ElementsMatch(t, []string{postdomain.FieldSlug, postdomain.FieldSEO}, latest.ChangedFields)

	snapshot, err := postdomain.DecodeSEO(latest.Snapshot.SEO)
	require.NoError(t, err)
	require.Equal(t, "Search title", snapshot.Title)
}
