// Package postseo adapts site settings and media storage to the post SEO ports.
package postseo

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaports "github.com/turahe/blog-api/internal/core/media/ports"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

// maxShareWidth is the widest social card image requested; larger images only add bytes.
const maxShareWidth = 1200

// SettingsReader is the part of the settings service the defaults need.
type SettingsReader interface {
	Values(ctx context.Context) (settingsdomain.Values, error)
}

// Defaults reads post SEO fallbacks from the site settings.
type Defaults struct {
	settings SettingsReader
}

// NewDefaults returns Defaults over settings.
func NewDefaults(settings SettingsReader) *Defaults {
	return &Defaults{settings: settings}
}

// SEODefaults returns the current site-level SEO fallbacks.
func (d *Defaults) SEODefaults(ctx context.Context) (postdomain.SEODefaults, error) {
	values, err := d.settings.Values(ctx)
	if err != nil {
		return postdomain.SEODefaults{}, err
	}

	base := values.String("site.canonical_base_url")
	if base == "" {
		base = values.String("site.public_url")
	}

	return postdomain.SEODefaults{
		SiteName:      values.String("site.name"),
		TitleTemplate: values.String("seo.title_template"),
		Description:   values.String("seo.default_description"),
		CanonicalBase: strings.TrimRight(base, "/"),
		ShareImageURL: values.String("seo.default_share_image_url"),
		TwitterCard:   postdomain.TwitterCard(values.String("seo.default_twitter_card")),
		AllowedHosts:  values.Strings("seo.canonical_allowed_hosts"),
	}, nil
}

// ImageURLs builds stable public URLs for social card images. Signed storage URLs expire
// and would break cached cards, so it uses the public transform redirect when image
// transforms are enabled and the public bucket URL otherwise.
type ImageURLs struct {
	media          mediaports.Repository
	apiBase        string
	transformWidth int
	publicBase     string
}

// NewImageURLs returns ImageURLs. apiBase is the API's public origin; widths is the
// transform allowlist, empty when transforms are disabled; publicBase is the public
// bucket URL, empty when the bucket is private.
func NewImageURLs(media mediaports.Repository, apiBase string, widths []int, publicBase string) *ImageURLs {
	width := 0

	for _, w := range widths {
		if w <= maxShareWidth && w > width {
			width = w
		}
	}

	return &ImageURLs{
		media:          media,
		apiBase:        strings.TrimRight(apiBase, "/"),
		transformWidth: width,
		publicBase:     strings.TrimRight(publicBase, "/"),
	}
}

// ImageURL returns the asset's public URL, or "" when it is missing, not ready, not an
// image, or has no public URL.
func (u *ImageURLs) ImageURL(ctx context.Context, mediaID uuid.UUID) (string, error) {
	if u == nil || u.media == nil {
		return "", nil
	}

	asset, err := u.media.GetByID(ctx, mediaID)
	if errors.Is(err, mediadomain.ErrNotFound) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	if asset.DeletedAt != nil || asset.Status != mediadomain.StatusReady || !strings.HasPrefix(asset.ContentType, "image/") {
		return "", nil
	}

	if _, ok := mediadomain.TransformableTypes[asset.ContentType]; ok && u.transformWidth > 0 && u.apiBase != "" {
		query := url.Values{"w": {strconv.Itoa(u.transformWidth)}, "format": {"jpeg"}}
		return u.apiBase + "/api/v1/media/" + asset.UUID.String() + "/transform?" + query.Encode(), nil
	}

	if u.publicBase != "" {
		return u.publicBase + "/" + strings.TrimLeft(asset.StorageKey, "/"), nil
	}

	return "", nil
}
