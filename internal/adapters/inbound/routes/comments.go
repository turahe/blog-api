package routes

import (
	"github.com/gin-gonic/gin"
)

// registerComments binds comment reads (anonymous), writes that allow guests (optional auth),
// and owner-only mutations (required auth).
func registerComments(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	pub, self := GroupPublic, GroupSelfService

	none := v1.Group("")
	get(none, "/posts/:param1/comments", "public.posts.comments.list", pub, AuthNone, c, c.Comments.PostList)
	get(none, "/comments/:param1", "public.comments.get", pub, AuthNone, c, c.Comments.Get)

	optional := v1.Group("")
	optional.Use(auth.Optional...)
	post(optional, "/posts/:param1/comments", "public.posts.comments.create", pub, AuthOptional, c, c.Comments.PostCreate)
	post(optional, "/comments/:param1/flag", "public.comments.flag", pub, AuthOptional, c, c.Comments.Flag)

	required := v1.Group("")
	required.Use(auth.Required...)
	get(required, "/me/comments", "self.comments.list", self, AuthRequired, c, c.Comments.MeList)
	patch(required, "/comments/:param1", "self.comments.patch", self, AuthRequired, c, c.Comments.Patch)
	del(required, "/comments/:param1", "self.comments.delete", self, AuthRequired, c, c.Comments.Delete)
	post(required, "/comments/:param1/upvote", "self.comments.upvote", self, AuthRequired, c, c.Comments.Upvote)
}
