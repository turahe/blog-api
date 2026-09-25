package responses

import (
	"github.com/gin-gonic/gin"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

// PostSEO is the admin view of a post's SEO. Every field is present; "" and null mean the
// rendered value falls back to one derived from the post or the site settings.
func PostSEO(view postservice.SEOView) gin.H {
	seo := view.SEO

	keywords := seo.Keywords
	if keywords == nil {
		keywords = []string{}
	}

	warnings := view.Warnings
	if warnings == nil {
		warnings = []postdomain.FieldViolation{}
	}

	return gin.H{
		"post_id":             view.PostUUID,
		"slug":                view.Slug,
		"seo_title":           seo.Title,
		"seo_description":     seo.Description,
		"seo_keywords":        keywords,
		"og_title":            seo.OGTitle,
		"og_description":      seo.OGDescription,
		"og_image_id":         uuidOrNil(seo.OGImageUUID),
		"og_url":              seo.OGURL,
		"twitter_card":        string(seo.TwitterCard),
		"twitter_title":       seo.TwitterTitle,
		"twitter_description": seo.TwitterDescription,
		"twitter_image_id":    uuidOrNil(seo.TwitterImageUUID),
		"twitter_creator":     seo.TwitterCreator,
		"canonical_url":       seo.CanonicalURL,
		"robots_noindex":      seo.RobotsNoindex,
		"robots_nofollow":     seo.RobotsNofollow,
		"warnings":            warnings,
	}
}
