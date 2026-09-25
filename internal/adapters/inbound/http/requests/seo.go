package requests

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

// NullableUUID tells an absent field (leave unchanged) from null (clear) and a value.
type NullableUUID struct {
	Present bool
	Value   *uuid.UUID
}

// UnmarshalJSON is only called when the key is present, including for null.
func (n *NullableUUID) UnmarshalJSON(data []byte) error {
	n.Present = true

	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		n.Value = nil
		return nil
	}

	var id uuid.UUID
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}

	n.Value = &id

	return nil
}

// UpdatePostSEO is PUT /api/v1/admin/posts/{id}/seo. Omitted fields stay unchanged, ""
// clears a text field, and null clears an image. Field rules are checked by the service
// so every invalid field is reported together.
type UpdatePostSEO struct {
	SEOTitle           *string      `json:"seoTitle"`
	SEODescription     *string      `json:"seoDescription"`
	SEOKeywords        *[]string    `json:"seoKeywords"`
	Slug               *string      `json:"slug"`
	OGTitle            *string      `json:"ogTitle"`
	OGDescription      *string      `json:"ogDescription"`
	OGImageID          NullableUUID `json:"ogImageId"          swaggertype:"string" format:"uuid"`
	OGURL              *string      `json:"ogUrl"`
	TwitterCard        *string      `json:"twitterCard"`
	TwitterTitle       *string      `json:"twitterTitle"`
	TwitterDescription *string      `json:"twitterDescription"`
	TwitterImageID     NullableUUID `json:"twitterImageId"     swaggertype:"string" format:"uuid"`
	TwitterCreator     *string      `json:"twitterCreator"`
	CanonicalURL       *string      `json:"canonicalUrl"`
	RobotsNoindex      *bool        `json:"robotsNoindex"`
	RobotsNofollow     *bool        `json:"robotsNofollow"`
}

// Patch converts the request to the domain patch.
func (r UpdatePostSEO) Patch() postdomain.SEOPatch {
	patch := postdomain.SEOPatch{
		Title:              r.SEOTitle,
		Description:        r.SEODescription,
		Keywords:           r.SEOKeywords,
		Slug:               r.Slug,
		OGTitle:            r.OGTitle,
		OGDescription:      r.OGDescription,
		OGImage:            postdomain.OptionalUUID{Present: r.OGImageID.Present, Value: r.OGImageID.Value},
		OGURL:              r.OGURL,
		TwitterTitle:       r.TwitterTitle,
		TwitterDescription: r.TwitterDescription,
		TwitterImage:       postdomain.OptionalUUID{Present: r.TwitterImageID.Present, Value: r.TwitterImageID.Value},
		TwitterCreator:     r.TwitterCreator,
		CanonicalURL:       r.CanonicalURL,
		RobotsNoindex:      r.RobotsNoindex,
		RobotsNofollow:     r.RobotsNofollow,
	}

	if r.TwitterCard != nil {
		card := postdomain.TwitterCard(*r.TwitterCard)
		patch.TwitterCard = &card
	}

	return patch
}

// PreviewPostSEO is POST /api/v1/admin/posts/{id}/seo/preview: SEO changes plus
// optional unsaved post title and excerpt. Nothing is saved.
type PreviewPostSEO struct {
	UpdatePostSEO

	Title   *string `json:"title"   binding:"omitempty,max=255"`
	Excerpt *string `json:"excerpt" binding:"omitempty,max=2000"`
}
