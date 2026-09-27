package service

import (
	"context"
	"errors"
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
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

type memSEO struct {
	byPost  map[uuid.UUID]postdomain.SEO
	saves   int
	getErr  error
	saveErr error
}

func (m *memSEO) Get(_ context.Context, postID uuid.UUID) (postdomain.SEO, error) {
	return m.byPost[postID], m.getErr
}

func (m *memSEO) Save(_ context.Context, postID uuid.UUID, seo postdomain.SEO, _ time.Time) error {
	if m.saveErr != nil {
		return m.saveErr
	}

	m.saves++
	m.byPost[postID] = seo

	return nil
}

type staticDefaults postdomain.SEODefaults

func (d staticDefaults) SEODefaults(context.Context) (postdomain.SEODefaults, error) {
	return postdomain.SEODefaults(d), nil
}

type failingDefaults struct{ err error }

func (d failingDefaults) SEODefaults(context.Context) (postdomain.SEODefaults, error) {
	return postdomain.SEODefaults{}, d.err
}

type mapImages map[uuid.UUID]string

func (m mapImages) ImageURL(_ context.Context, id uuid.UUID) (string, error) { return m[id], nil }

// failingImages fails for the ids in failFor and resolves every other id.
type failingImages struct {
	failFor map[uuid.UUID]bool
	err     error
}

func (f failingImages) ImageURL(_ context.Context, id uuid.UUID) (string, error) {
	if f.failFor[id] {
		return "", f.err
	}

	return "https://cdn.example.com/" + id.String(), nil
}

// seoMedia is a media repository that only answers GetByID; err, when set, is returned
// for every lookup.
type seoMedia struct {
	mediaports.Repository

	assets map[uuid.UUID]mediadomain.MediaAsset
	err    error
}

func (m seoMedia) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	if m.err != nil {
		return mediadomain.MediaAsset{}, m.err
	}

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

func TestHomeSEOMetaRendersSiteDefaults(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)

	meta, err := f.svc.HomeSEOMeta(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Blog", meta.Title)
	assert.Equal(t, "https://blog.example.com/", meta.Canonical)
	assert.Equal(t, "website", meta.OpenGraph["og:type"])

	meta, err = New(nil, nil, nil).HomeSEOMeta(t.Context())
	require.NoError(t, err, "without SEO settings the home meta still renders")
	assert.Equal(t, "index,follow", meta.Robots)
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

func TestSEORequiresSEOSupport(t *testing.T) {
	t.Parallel()

	postID, authorID := uuid.New(), uuid.New()
	svc := New(newFakePostRepo(postdomain.Post{UUID: postID, AuthorUUID: authorID, Slug: "t", Version: 1}), randomIDs{}, fixedClock{})

	tests := []struct {
		name string
		call func() error
	}{
		{name: "get", call: func() error {
			_, err := svc.GetSEO(t.Context(), postID, authorID, false)
			return err
		}},
		{name: "update", call: func() error {
			_, err := svc.UpdateSEO(t.Context(), postID, authorID, false, false, postdomain.SEOPatch{})
			return err
		}},
		{name: "preview", call: func() error {
			_, err := svc.PreviewSEO(t.Context(), postID, authorID, false, SEODraft{})
			return err
		}},
		{name: "meta", call: func() error {
			_, err := svc.SEOMeta(t.Context(), "t")
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, tc.call(), ErrValidation)
		})
	}
}

// seoCalls exercises every SEO read path of f's post; the post must be published for meta.
func seoCalls(f *seoFixture) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"get": func(ctx context.Context) error {
			_, err := f.svc.GetSEO(ctx, f.post.UUID, f.authorID, false)
			return err
		},
		"update": func(ctx context.Context) error {
			_, err := f.svc.UpdateSEO(ctx, f.post.UUID, f.authorID, false, false, postdomain.SEOPatch{Title: new("x")})
			return err
		},
		"preview": func(ctx context.Context) error {
			_, err := f.svc.PreviewSEO(ctx, f.post.UUID, f.authorID, false, SEODraft{})
			return err
		},
		"meta": func(ctx context.Context) error {
			_, err := f.svc.SEOMeta(ctx, f.post.Slug)
			return err
		},
		"home": func(ctx context.Context) error {
			_, err := f.svc.HomeSEOMeta(ctx)
			return err
		},
	}
}

func TestSEODependencyFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("settings unavailable")

	tests := []struct {
		name    string
		breakIt func(*seoFixture)
		calls   []string
	}{
		{
			name:    "seo read fails",
			breakIt: func(f *seoFixture) { f.seo.getErr = failure },
			calls:   []string{"get", "update", "preview", "meta"},
		},
		{
			name:    "site defaults fail",
			breakIt: func(f *seoFixture) { f.svc.WithSEO(f.seo, failingDefaults{err: failure}, mapImages{}) },
			calls:   []string{"update", "preview", "meta", "home"},
		},
		{
			name:    "tag list fails while rendering",
			breakIt: func(f *seoFixture) { f.svc.WithTags(&fakeTagLinker{listErr: failure}) },
			calls:   []string{"preview", "meta"},
		},
	}

	for _, tc := range tests {
		for _, call := range tc.calls {
			t.Run(tc.name+"/"+call, func(t *testing.T) {
				t.Parallel()

				f := newSEOFixture(t)
				_, err := f.svc.Publish(t.Context(), f.post.UUID)
				require.NoError(t, err)
				tc.breakIt(f)

				require.ErrorIs(t, seoCalls(f)[call](t.Context()), failure)
			})
		}
	}
}

func TestUpdateSEOCommitFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("store unavailable")

	tests := []struct {
		name        string
		slugAllowed bool
		patch       postdomain.SEOPatch
		breakIt     func(*seoFixture)
	}{
		{
			name: "rename fails", slugAllowed: true, patch: postdomain.SEOPatch{Slug: new("renamed")},
			breakIt: func(f *seoFixture) { f.repo.updateErr = failure },
		},
		{
			name: "slug lookup fails", slugAllowed: true, patch: postdomain.SEOPatch{Slug: new("renamed")},
			breakIt: func(f *seoFixture) { f.repo.slugTakenErr = failure },
		},
		{
			name: "seo save fails", patch: postdomain.SEOPatch{Title: new("New")},
			breakIt: func(f *seoFixture) { f.seo.saveErr = failure },
		},
		{
			name: "revision fails", patch: postdomain.SEOPatch{Title: new("New")},
			breakIt: func(f *seoFixture) { f.revs.latestErr = failure },
		},
		{
			name: "image lookup fails", patch: postdomain.SEOPatch{OGImage: postdomain.OptionalUUID{Present: true, Value: new(uuid.New())}},
			breakIt: func(f *seoFixture) { f.svc.WithMedia(&fakePostMedia{}, seoMedia{err: failure}) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newSEOFixture(t)
			tc.breakIt(f)

			_, err := f.svc.UpdateSEO(t.Context(), f.post.UUID, f.authorID, false, tc.slugAllowed, tc.patch)

			require.ErrorIs(t, err, failure)
			assert.NotContains(t, f.events.Types(), event.PostSEOUpdated)
		})
	}
}

func TestUpdateSEOFailsWhenEventCannotBeRecorded(t *testing.T) {
	t.Parallel()

	failure := errors.New("outbox unavailable")
	post := lifecyclePost(postdomain.StatusDraft)
	seo := &memSEO{byPost: map[uuid.UUID]postdomain.SEO{}}
	svc := New(newFakePostRepo(post), randomIDs{}, fixedClock{now: lifecycleNow}).
		WithEvents((&eventtest.Recorder{Err: failure}).Unit()).
		WithSEO(seo, staticDefaults{}, mapImages{})

	_, err := svc.UpdateSEO(t.Context(), post.UUID, post.AuthorUUID, true, false, postdomain.SEOPatch{Title: new("New")})

	require.ErrorIs(t, err, failure)
}

func TestUpdateSEOWithoutMediaSkipsImageChecks(t *testing.T) {
	t.Parallel()

	post := lifecyclePost(postdomain.StatusDraft)
	seo := &memSEO{byPost: map[uuid.UUID]postdomain.SEO{}}
	svc := New(newFakePostRepo(post), randomIDs{}, fixedClock{now: lifecycleNow}).WithSEO(seo, staticDefaults{}, mapImages{})
	image := uuid.New()

	view, err := svc.UpdateSEO(t.Context(), post.UUID, post.AuthorUUID, true, false, postdomain.SEOPatch{
		OGImage: postdomain.OptionalUUID{Present: true, Value: &image},
	})

	require.NoError(t, err)
	assert.Equal(t, &image, view.SEO.OGImageUUID)
	assert.Equal(t, &image, seo.byPost[post.UUID].OGImageUUID)
}

func TestPreviewSEOAppliesDraftPostFields(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)

	preview, err := f.svc.PreviewSEO(t.Context(), f.post.UUID, f.authorID, false, SEODraft{
		Patch:   postdomain.SEOPatch{Slug: new("  Draft-Slug ")},
		Excerpt: new("Draft excerpt"),
	})
	require.NoError(t, err)

	assert.Equal(t, "Draft excerpt", preview.Search.Description)
	assert.Equal(t, "https://blog.example.com/posts/draft-slug", preview.Search.URL)
	assert.Equal(t, f.post.Slug, f.repo.posts[f.post.UUID].Slug, "previews rename nothing")

	_, err = f.svc.PreviewSEO(t.Context(), f.post.UUID, uuid.New(), false, SEODraft{})
	require.ErrorIs(t, err, postdomain.ErrNotFound)
}

func TestPreviewSEOResolvesImages(t *testing.T) {
	t.Parallel()

	failure := errors.New("cdn unavailable")

	tests := []struct {
		name    string
		failFor func(og, twitter, cover uuid.UUID) uuid.UUID
		want    error
	}{
		{name: "all resolve", failFor: func(_, _, _ uuid.UUID) uuid.UUID { return uuid.Nil }},
		{name: "og image fails", failFor: func(og, _, _ uuid.UUID) uuid.UUID { return og }, want: failure},
		{name: "twitter image fails", failFor: func(_, twitter, _ uuid.UUID) uuid.UUID { return twitter }, want: failure},
		{name: "cover image fails", failFor: func(_, _, cover uuid.UUID) uuid.UUID { return cover }, want: failure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newSEOFixture(t)
			og, twitter, cover := uuid.New(), uuid.New(), uuid.New()
			post := f.repo.posts[f.post.UUID]
			post.CoverImageMediaUUID = &cover
			f.repo.posts[f.post.UUID] = post
			images := failingImages{failFor: map[uuid.UUID]bool{tc.failFor(og, twitter, cover): true}, err: failure}
			f.svc.WithSEO(f.seo, staticDefaults{}, images)

			preview, err := f.svc.PreviewSEO(t.Context(), f.post.UUID, f.authorID, false, SEODraft{Patch: postdomain.SEOPatch{
				OGImage:      postdomain.OptionalUUID{Present: true, Value: &og},
				TwitterImage: postdomain.OptionalUUID{Present: true, Value: &twitter},
			}})

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "https://cdn.example.com/"+og.String(), preview.OG.ImageURL)
			assert.Equal(t, "https://cdn.example.com/"+twitter.String(), preview.Twitter.ImageURL)
		})
	}
}

func TestSEOMetaRendersTagsAndRejectsBlankSlug(t *testing.T) {
	t.Parallel()

	f := newSEOFixture(t)
	f.svc.WithTags(&fakeTagLinker{listTags: []tagdomain.Tag{{Name: "go"}, {Name: "testing"}}})
	_, err := f.svc.Publish(t.Context(), f.post.UUID)
	require.NoError(t, err)

	meta, err := f.svc.SEOMeta(t.Context(), f.post.Slug)
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "testing"}, meta.OpenGraph["article:tag"])

	_, err = f.svc.SEOMeta(t.Context(), "   ")
	require.ErrorIs(t, err, postdomain.ErrNotFound)
}
