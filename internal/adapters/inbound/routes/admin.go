package routes

import (
	"github.com/gin-gonic/gin"
)

// registerAdmin binds /admin routes (login is anonymous; the rest require auth).
func registerAdmin(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAdmin

	none := v1.Group("")
	post(none, "/admin/auth/login", "admin.auth.login", g, AuthNone, c, c.Auth.AdminLogin)

	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	get(admin, "/users", "admin.users.list", g, AuthRequired, c, c.Users.AdminUsersList)
	post(admin, "/users", "admin.users.create", g, AuthRequired, c, c.Users.AdminCreate)
	post(admin, "/users/:param1/password/admin-reset", "admin.users.password.admin_reset", g, AuthRequired, c, c.Users.AdminPasswordReset)
	get(admin, "/users/:param1/profile", "admin.users.profile.get", g, AuthRequired, c, c.Users.AdminProfileGet)
	patch(admin, "/users/:param1/profile", "admin.users.profile.patch", g, AuthRequired, c, c.Users.AdminProfilePatch)
	get(admin, "/users/:param1/activity", "admin.users.activity.list", g, AuthRequired, c, c.Activity.AdminUserList)
	get(admin, "/users/:param1/roles", "admin.users.roles.list", g, AuthRequired, c, c.Roles.UserRolesList)
	post(admin, "/users/:param1/roles", "admin.users.roles.assign", g, AuthRequired, c, c.Roles.UserRolesAssign)
	del(admin, "/users/:param1/roles/:param2", "admin.users.roles.revoke", g, AuthRequired, c, c.Roles.UserRoleRevoke)

	get(admin, "/roles", "admin.roles.list", g, AuthRequired, c, c.Roles.List)
	post(admin, "/roles", "admin.roles.create", g, AuthRequired, c, c.Roles.Create)
	get(admin, "/roles/:param1", "admin.roles.get", g, AuthRequired, c, c.Roles.Get)
	patch(admin, "/roles/:param1", "admin.roles.update", g, AuthRequired, c, c.Roles.Update)
	del(admin, "/roles/:param1", "admin.roles.delete", g, AuthRequired, c, c.Roles.Delete)
	put(admin, "/roles/:param1/permissions", "admin.roles.permissions.set", g, AuthRequired, c, c.Roles.SetPermissions)
	get(admin, "/permissions", "admin.permissions.list", g, AuthRequired, c, c.Roles.Permissions)

	get(admin, "/posts", "admin.posts.list", g, AuthRequired, c, c.Posts.AdminList)
	post(admin, "/posts", "admin.posts.create", g, AuthRequired, c, c.Posts.AdminCreate)
	post(admin, "/posts/:param1/publish", "admin.posts.publish", g, AuthRequired, c, c.Posts.AdminPublish)
	post(admin, "/posts/:param1/unpublish", "admin.posts.unpublish", g, AuthRequired, c, c.Posts.AdminUnpublish)
	post(admin, "/posts/:param1/archive", "admin.posts.archive", g, AuthRequired, c, c.Posts.AdminArchive)
	post(admin, "/posts/:param1/restore", "admin.posts.restore", g, AuthRequired, c, c.Posts.AdminRestore)
	patch(admin, "/posts/:param1", "admin.posts.update", g, AuthRequired, c, c.Posts.AdminUpdate)
	del(admin, "/posts/:param1", "admin.posts.delete", g, AuthRequired, c, c.Posts.AdminDelete)
	patch(admin, "/posts/:param1/media", "admin.posts.media.replace", g, AuthRequired, c, c.Posts.AdminMediaReplace)
	get(admin, "/posts/:param1/revisions", "admin.posts.revisions.list", g, AuthRequired, c, c.Posts.RevisionsList)
	get(admin, "/posts/:param1/revisions/:param2", "admin.posts.revisions.get", g, AuthRequired, c, c.Posts.RevisionGet)
	post(admin, "/posts/:param1/revisions/:param2/restore", "admin.posts.revisions.restore", g, AuthRequired, c, c.Posts.RevisionRestore)
	get(admin, "/posts/:param1/seo", "admin.posts.seo.get", g, AuthRequired, c, c.Posts.SEOGet)
	put(admin, "/posts/:param1/seo", "admin.posts.seo.update", g, AuthRequired, c, c.Posts.SEOUpdate)
	post(admin, "/posts/:param1/seo/preview", "admin.posts.seo.preview", g, AuthRequired, c, c.Posts.SEOPreview)

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

	get(admin, "/comments", "admin.comments.list", g, AuthRequired, c, c.Comments.AdminList)
	get(admin, "/comments/stats", "admin.comments.stats", g, AuthRequired, c, c.Comments.AdminStats)
	get(admin, "/comments/:param1", "admin.comments.get", g, AuthRequired, c, c.Comments.AdminGet)
	post(admin, "/comments/:param1/moderate", "admin.comments.moderate", g, AuthRequired, c, c.Comments.AdminModerate)
	post(admin, "/comments/bulk-moderate", "admin.comments.bulk_moderate", g, AuthRequired, c, c.Comments.AdminBulkModerate)
	del(admin, "/comments/:param1", "admin.comments.delete", g, AuthRequired, c, c.Comments.AdminHardDelete)

	get(admin, "/settings", "admin.settings.get", g, AuthRequired, c, c.Settings.Get)
	get(admin, "/settings/history", "admin.settings.history", g, AuthRequired, c, c.Settings.History)
	put(admin, "/settings", "admin.settings.put", g, AuthRequired, c, c.Settings.Put)
}
