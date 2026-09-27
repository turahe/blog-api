package postseo

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

type fakeSettings map[string]any

func (f fakeSettings) Values(context.Context) (settingsdomain.Values, error) {
	return settingsdomain.NewValues(f), nil
}

type failingSettings struct{ err error }

func (f failingSettings) Values(context.Context) (settingsdomain.Values, error) {
	return settingsdomain.Values{}, f.err
}

type fakeMedia struct {
	mediaports.Repository

	assets map[uuid.UUID]mediadomain.MediaAsset
	err    error
}

func (f fakeMedia) GetByID(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	if f.err != nil {
		return mediadomain.MediaAsset{}, f.err
	}

	asset, ok := f.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, mediadomain.ErrNotFound
	}

	return asset, nil
}

func TestDefaultsPreferCanonicalBase(t *testing.T) {
	t.Parallel()

	settings := fakeSettings{
		"site.name": "Blog", "site.public_url": "https://www.example.com", "seo.title_template": "{title} | {site}",
		"seo.default_twitter_card": "summary", "seo.canonical_allowed_hosts": []string{"cdn.example.com"},
	}

	got, err := NewDefaults(settings).SEODefaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://www.example.com", got.CanonicalBase)
	assert.Equal(t, postdomain.TwitterSummary, got.TwitterCard)
	assert.Equal(t, []string{"cdn.example.com"}, got.AllowedHosts)

	settings["site.canonical_base_url"] = "https://example.com/"
	got, err = NewDefaults(settings).SEODefaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", got.CanonicalBase)
}

func TestDefaultsSurfaceSettingsErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("settings down")

	got, err := NewDefaults(failingSettings{err: boom}).SEODefaults(t.Context())
	require.ErrorIs(t, err, boom)
	assert.Equal(t, postdomain.SEODefaults{}, got)
}

func TestDefaultsReadHomeSettings(t *testing.T) {
	t.Parallel()

	settings := fakeSettings{
		"site.tagline": "Notes on Go", "seo.home_title": "Welcome", "seo.home_description": "Home description",
		"seo.home_keywords": []string{"go", "web"}, "seo.home_share_image_url": "https://cdn.example.com/home.png",
	}

	got, err := NewDefaults(settings).SEODefaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Notes on Go", got.Tagline)
	assert.Equal(t, postdomain.HomeSEO{
		Title: "Welcome", Description: "Home description", Keywords: []string{"go", "web"},
		ShareImageURL: "https://cdn.example.com/home.png",
	}, got.Home)
}

func TestImageURLs(t *testing.T) {
	t.Parallel()

	png, gif, pending, video := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	media := fakeMedia{assets: map[uuid.UUID]mediadomain.MediaAsset{
		png:     {UUID: png, ContentType: "image/png", Status: mediadomain.StatusReady, StorageKey: "a/b.png"},
		gif:     {UUID: gif, ContentType: "image/gif", Status: mediadomain.StatusReady, StorageKey: "c.gif"},
		pending: {UUID: pending, ContentType: "image/png", Status: mediadomain.StatusPending},
		video:   {UUID: video, ContentType: "video/mp4", Status: mediadomain.StatusReady},
	}}

	transforms := NewImageURLs(media, "https://api.example.com/", []int{256, 1024, 1280}, "https://cdn.example.com")
	got, err := transforms.ImageURL(t.Context(), png)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/api/v1/media/"+png.String()+"/transform?format=jpeg&w=1024", got)

	for _, id := range []uuid.UUID{pending, video, uuid.New()} {
		got, err := transforms.ImageURL(t.Context(), id)
		require.NoError(t, err)
		assert.Empty(t, got)
	}

	bucket := NewImageURLs(media, "https://api.example.com", nil, "https://cdn.example.com/")
	got, err = bucket.ImageURL(t.Context(), png)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/a/b.png", got)

	private := NewImageURLs(media, "https://api.example.com", nil, "")
	got, err = private.ImageURL(t.Context(), png)
	require.NoError(t, err)
	assert.Empty(t, got)

	var none *ImageURLs

	got, err = none.ImageURL(t.Context(), png)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestImageURLsSurfaceRepositoryErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("db down")
	urls := NewImageURLs(fakeMedia{err: boom}, "https://api.example.com", []int{256}, "https://cdn.example.com")

	got, err := urls.ImageURL(t.Context(), uuid.New())
	require.ErrorIs(t, err, boom)
	assert.Empty(t, got)
}
