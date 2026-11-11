package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterPublicContentRouter mounts the anonymous-safe public-content routes
// (posts, categories, tags, media transforms, public settings) under /api/v1.
func RegisterPublicContentRouter(router *gin.RouterGroup, c Controllers) {
	g := GroupPublic
	n := AuthNone

	get(router, "/posts", "public.posts.list", g, n, c, c.Posts.PublicList)
	get(router, "/posts/:param1", "public.posts.get", g, n, c, c.Posts.PublicGet)
	get(router, "/posts/:param1/seo-meta", "public.posts.seo_meta", g, n, c, c.Posts.PublicSEOMeta)
	get(router, "/home/seo-meta", "public.home.seo_meta", g, n, c, c.Posts.PublicHomeSEOMeta)
	get(router, "/categories", "public.categories.list", g, n, c, c.Cats.PublicList)
	get(router, "/categories/:param1", "public.categories.get", g, n, c, c.Cats.PublicGet)
	get(router, "/tags", "public.tags.list", g, n, c, c.Tags.PublicList)
	get(router, "/media/:param1", "public.media.get", g, n, c, c.Media.PublicGet)
	get(router, "/media/:param1/transform", "public.media.transform", g, n, c, c.Media.PublicTransform)
	get(router, "/settings", "public.settings.get", g, n, c, c.Settings.Public)
}
