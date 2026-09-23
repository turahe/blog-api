package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterAdmin binds /admin routes (login is anonymous; the rest require auth).
func RegisterAdmin(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAdmin

	none := v1.Group("")
	post(none, "/admin/auth/login", "admin.auth.login", g, AuthNone, c, nil)

	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	get(admin, "/users", "admin.users.list", g, AuthRequired, c, c.Users.AdminUsersList)

	get(admin, "/posts", "admin.posts.list", g, AuthRequired, c, c.Posts.AdminList)
	post(admin, "/posts", "admin.posts.create", g, AuthRequired, c, c.Posts.AdminCreate)
	post(admin, "/posts/:param1/publish", "admin.posts.publish", g, AuthRequired, c, c.Posts.AdminPublish)
	patch(admin, "/posts/:param1", "admin.posts.update", g, AuthRequired, c, c.Posts.AdminUpdate)
	patch(admin, "/posts/:param1/media", "admin.posts.media.replace", g, AuthRequired, c, c.Posts.AdminMediaReplace)

	get(admin, "/categories", "admin.categories.list", g, AuthRequired, c, c.Cats.AdminList)
	post(admin, "/categories", "admin.categories.create", g, AuthRequired, c, c.Cats.AdminCreate)
	patch(admin, "/categories/:param1", "admin.categories.update", g, AuthRequired, c, c.Cats.AdminUpdate)
	del(admin, "/categories/:param1", "admin.categories.delete", g, AuthRequired, c, c.Cats.AdminDelete)
	post(admin, "/categories/:param1/move", "admin.categories.move", g, AuthRequired, c, c.Cats.AdminMove)

	post(admin, "/tags", "admin.tags.create", g, AuthRequired, c, c.Tags.AdminCreate)
	patch(admin, "/tags/:param1", "admin.tags.update", g, AuthRequired, c, c.Tags.AdminUpdate)
	post(admin, "/tags/:param1/merge", "admin.tags.merge", g, AuthRequired, c, c.Tags.AdminMerge)
	del(admin, "/tags/:param1", "admin.tags.delete", g, AuthRequired, c, c.Tags.AdminDelete)

	get(admin, "/media", "admin.media.list", g, AuthRequired, c, c.Media.AdminList)
	post(admin, "/media", "admin.media.create", g, AuthRequired, c, c.Media.AdminCreate)
	post(admin, "/media/:param1/complete", "admin.media.complete", g, AuthRequired, c, c.Media.AdminComplete)
	patch(admin, "/media/:param1/tags", "admin.media.tags.patch", g, AuthRequired, c, c.Media.AdminTagsPatch)
	del(admin, "/media/:param1", "admin.media.delete", g, AuthRequired, c, c.Media.AdminDelete)
}
