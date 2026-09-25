package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type memSEO struct {
	byPost map[uuid.UUID]postdomain.SEO
	saves  int
}

func (m *memSEO) Get(_ context.Context, postID uuid.UUID) (postdomain.SEO, error) {
	return m.byPost[postID], nil
}

func (m *memSEO) Save(_ context.Context, postID uuid.UUID, seo postdomain.SEO, _ time.Time) error {
	m.saves++
	m.byPost[postID] = seo

	return nil
}

type staticDefaults postdomain.SEODefaults

func (d staticDefaults) SEODefaults(context.Context) (postdomain.SEODefaults, error) {
	return postdomain.SEODefaults(d), nil
}

type mapImages map[uuid.UUID]string

func (m mapImages) ImageURL(_ context.Context, id uuid.UUID) (string, error) { return m[id], nil }

// seoMedia is a media repository that only answers GetByID.
type seoMedia struct {
	mediaports.Repository

	assets map[uuid.UUID]mediadomain.MediaAsset
}

func (m seoMedia) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, ok := m.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, mediadomain.ErrNotFound
	}

	return asset, nil
}

type seoFixture struct {
	svc      *PostService
	repo     *fakePostRepo
	seo      *memSEO
	revs     *memRevisions
	events   *eventtest.Recorder
	media    seoMedia
	authorID uuid.UUID
	post     postdomain.Post
}

func newSEOFixture(t *testing.T) *seoFixture {
	t.Helper()

	f := &seoFixture{
		repo:     newFakePostRepo(),
		seo:      &memSEO{byPost: map[uuid.UUID]postdomain.SEO{}},
		revs:     newMemRevisions(),
		events:   &eventtest.Recorder{},
		media:    seoMedia{assets: map[uuid.UUID]mediadomain.MediaAsset{}},
		authorID: uuid.New(),
	}
	f.svc = revisionService(f.repo, f.revs, f.events).
		WithMedia(&fakePostMedia{}, f.media).
		WithSEO(f.seo, staticDefaults{
			SiteName: "Blog", TitleTemplate: "{title} | {site}", CanonicalBase: "https://blog.example.com",
		}, mapImages{})

	post, _, err := f.svc.CreateDraft(t.Context(), f.authorID, "First post", "", "", "body", nil, nil)
	require.NoError(t, err)

	f.post = post

	return f
}

func (f *seoFixture) addImage(contentType, status string) uuid.UUID {
	id := uuid.New()
	f.media.assets[id] = mediadomain.MediaAsset{UUID: id, ContentType: contentType, Status: status}
	f.revs.live[id] = true

	return id
}

func TestUpdateSEOSavesRecordsRevisionAndEvent(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)
	image := f.addImage("image/png", mediadomain.StatusReady)

	view, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{
		Title:   new("  Better   title "),
		OGImage: postdomain.OptionalUUID{Present: true, Value: &image},
	})
	require.NoError(t, err)

	assert.Equal(t, "Better title", view.SEO.Title)
	assert.Equal(t, &image, f.seo.byPost[f.post.UUID].OGImageUUID)
	assert.Contains(t, f.events.Types(), event.PostSEOUpdated)
	assert.NotContains(t, f.events.Types(), event.PostSlugChanged)

	revs := f.revs.byPost[f.post.UUID]
	require.Len(t, revs, 2)
	assert.Equal(t, postdomain.RevisionUpdate, revs[1].Type)
	assert.Equal(t, []string{postdomain.FieldSEO}, revs[1].ChangedFields)

	saves := f.seo.saves
	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{Title: new("Better title")})
	require.NoError(t, err)
	assert.Equal(t, saves, f.seo.saves, "an unchanged patch writes nothing")
	assert.Len(t, f.revs.byPost[f.post.UUID], 2)
}

func TestUpdateSEOReportsAllViolations(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)
	video := f.addImage("video/mp4", mediadomain.StatusReady)
	missing := uuid.New()

	_, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, true, postdomain.SEOPatch{
		Title:        new("<b>bold</b>"),
		CanonicalURL: new("https://other.example.net/x"),
		OGImage:      postdomain.OptionalUUID{Present: true, Value: &video},
		TwitterImage: postdomain.OptionalUUID{Present: true, Value: &missing},
		Slug:         new("admin"),
	})

	var invalid *postdomain.SEOValidationError
	require.ErrorAs(t, err, &invalid)
	require.ErrorIs(t, err, ErrValidation)

	got := map[string]string{}
	for _, v := range invalid.Violations {
		got[v.Field] = v.Code
	}

	assert.Equal(t, map[string]string{
		"seo_title":        postdomain.SEOCodeMarkup,
		"canonical_url":    postdomain.SEOCodeHostNotAllowed,
		"og_image_id":      postdomain.SEOCodeNotImage,
		"twitter_image_id": postdomain.SEOCodeNotFound,
		"slug":             postdomain.SEOCodeSlugReserved,
	}, got)
	assert.Zero(t, f.seo.saves)
	assert.NotContains(t, f.events.Types(), event.PostSEOUpdated)
}

func TestUpdateSEOSlugNeedsPermissionAndMustBeFree(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)

	_, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{Slug: new("renamed")})
	require.ErrorIs(t, err, postdomain.ErrSlugEditForbidden)

	f.repo.slugTakenBySlug["taken"] = true
	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, true, postdomain.SEOPatch{Slug: new("taken")})

	var invalid *postdomain.SEOValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, postdomain.SEOCodeSlugTaken, invalid.Violations[0].Code)

	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{Slug: new(f.post.Slug)})
	require.NoError(t, err, "resubmitting the current slug needs no permission")

	view, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, true, postdomain.SEOPatch{Slug: new(" Renamed ")})
	require.NoError(t, err)
	assert.Equal(t, "renamed", view.Slug)
	assert.Equal(t, "renamed", f.repo.posts[f.post.UUID].Slug)
	assert.Contains(t, f.events.Types(), event.PostSlugChanged)
	assert.NotContains(t, f.events.Types(), event.PostSEOUpdated, "a slug-only change is not an SEO field change")
}

func TestSEOHidesOtherAuthorsPosts(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)
	stranger := uuid.New()

	_, err := f.svc.GetSEO(t.Context(), f.post.UUID, stranger, false)
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, stranger, false, true, postdomain.SEOPatch{Title: new("x")})
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	_, err = f.svc.GetSEO(t.Context(), f.post.UUID, stranger, true)
	require.NoError(t, err)
}

func TestRestoreRevisionRestoresSEOAndSkipsMissingImages(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)
	image := f.addImage("image/jpeg", mediadomain.StatusReady)

	_, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{
		Title: new("Original"), OGImage: postdomain.OptionalUUID{Present: true, Value: &image},
	})
	require.NoError(t, err)

	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{
		Title: new("Changed"), OGImage: postdomain.OptionalUUID{Present: true},
	})
	require.NoError(t, err)

	delete(f.revs.live, image)

	two := 2
	result, err := f.svc.RestoreRevision(t.Context(), f.post.UUID, postdomain.RevisionRef{Number: &two}, f.authorID, false, "")
	require.NoError(t, err)

	restored := f.seo.byPost[f.post.UUID]
	assert.Equal(t, "Original", restored.Title)
	assert.Nil(t, restored.OGImageUUID)
	assert.Equal(t, []uuid.UUID{image}, result.Skipped.Media)
}

func TestSEOMetaServesPublishedPostsOnly(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)

	_, err := f.svc.SEOMeta(t.Context(), f.post.Slug)
	require.ErrorIs(t, err, postdomain.ErrNotFound)

	_, err = f.svc.Publish(t.Context(), f.post.UUID)
	require.NoError(t, err)

	_, err = f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{
		RobotsNoindex: new(true),
	})
	require.NoError(t, err)

	meta, err := f.svc.SEOMeta(t.Context(), f.post.Slug)
	require.NoError(t, err)
	assert.Equal(t, "First post | Blog", meta.Title)
	assert.Equal(t, "https://blog.example.com/posts/"+f.post.Slug, meta.Canonical)
	assert.Equal(t, "noindex,follow", meta.Robots)
}

func TestPreviewSEOWritesNothing(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)

	preview, err := f.svc.PreviewSEO(t.Context(), f.post.UUID, f.authorID, false, SEODraft{
		Patch: postdomain.SEOPatch{Description: new("Draft description")},
		Title: new("Draft title"),
	})
	require.NoError(t, err)

	assert.Equal(t, "Draft title | Blog", preview.Search.Title)
	assert.Equal(t, "Draft description", preview.Search.Description)
	assert.Equal(t, "Draft title", preview.OG.Title)
	assert.Zero(t, f.seo.saves)
	assert.Len(t, f.revs.byPost[f.post.UUID], 1)

	_, err = f.svc.PreviewSEO(t.Context(), f.post.UUID, f.authorID, false, SEODraft{
		Patch: postdomain.SEOPatch{OGURL: new("ftp://x")},
	})

	var invalid *postdomain.SEOValidationError
	require.ErrorAs(t, err, &invalid)
}
