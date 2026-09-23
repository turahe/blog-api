package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterPublic binds anonymous-safe content routes under /api/v1.
func RegisterPublic(router gin.IRoutes, c Controllers) {
	g := GroupPublic
	n := AuthNone

	get(router, "/posts", "public.posts.list", g, n, c, c.Posts.PublicList)
	get(router, "/posts/:param1", "public.posts.get", g, n, c, c.Posts.PublicGet)
	get(router, "/categories", "public.categories.list", g, n, c, c.Cats.PublicList)
	get(router, "/categories/:param1", "public.categories.get", g, n, c, c.Cats.PublicGet)
	get(router, "/tags", "public.tags.list", g, n, c, c.Tags.PublicList)
	get(router, "/media/:param1", "public.media.get", g, n, c, c.Media.PublicGet)
}
