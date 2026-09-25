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

	return gin.H{
		"postId":             view.PostUUID,
		"slug":               view.Slug,
		"seoTitle":           seo.Title,
		"seoDescription":     seo.Description,
		"seoKeywords":        keywords,
		"ogTitle":            seo.OGTitle,
		"ogDescription":      seo.OGDescription,
		"ogImageId":          uuidOrNil(seo.OGImageUUID),
		"ogUrl":              seo.OGURL,
		"twitterCard":        string(seo.TwitterCard),
		"twitterTitle":       seo.TwitterTitle,
		"twitterDescription": seo.TwitterDescription,
		"twitterImageId":     uuidOrNil(seo.TwitterImageUUID),
		"twitterCreator":     seo.TwitterCreator,
		"canonicalUrl":       seo.CanonicalURL,
		"robotsNoindex":      seo.RobotsNoindex,
		"robotsNofollow":     seo.RobotsNofollow,
		"warnings":           FieldViolations(view.Warnings),
	}
}

// SEOMeta is the rendered meta a frontend emits as tags. The Open Graph and Twitter maps are
// keyed by their tag names (og:title, twitter:card), which are kept as they are.
func SEOMeta(meta postdomain.SEOMeta) gin.H {
	out := gin.H{
		"title":           meta.Title,
		"metaDescription": meta.MetaDescription,
		"robots":          meta.Robots,
		"openGraph":       meta.OpenGraph,
		"twitter":         meta.Twitter,
	}
	if meta.MetaKeywords != "" {
		out["metaKeywords"] = meta.MetaKeywords
	}

	if meta.Canonical != "" {
		out["canonical"] = meta.Canonical
	}

	return out
}

// SEOPreview renders the search snippet and social cards for the SEO editor.
func SEOPreview(p postdomain.SEOPreview) gin.H {
	return gin.H{
		"searchPreview": gin.H{"title": p.Search.Title, "url": p.Search.URL, "description": p.Search.Description},
		"ogPreview": gin.H{
			"title": p.OG.Title, "description": p.OG.Description, "imageUrl": p.OG.ImageURL,
			"url": p.OG.URL, "siteName": p.OG.SiteName,
		},
		"twitterPreview": gin.H{
			"card": p.Twitter.Card, "title": p.Twitter.Title, "description": p.Twitter.Description,
			"imageUrl": p.Twitter.ImageURL, "creator": p.Twitter.Creator,
		},
		"warnings": FieldViolations(p.Warnings),
	}
}
