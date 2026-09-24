package routes

import (
	"github.com/gin-gonic/gin"
)

// registerMe binds authenticated self-service routes and the public profile under /api/v1.
func registerMe(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupSelfService
	me := v1.Group("")
	me.Use(auth.Required...)

	get(me, "/me", "me.get", g, AuthRequired, c, c.Users.MeGet)
	put(me, "/me/password", "me.password.update", g, AuthRequired, c, c.Auth.MePasswordUpdate)
	patch(me, "/me/profile", "me.profile.patch", g, AuthRequired, c, c.Users.MeProfilePatch)
	post(me, "/me/avatar", "me.avatar.upload", g, AuthRequired, c, c.Users.MeAvatarUpload)
	del(me, "/me/avatar", "me.avatar.delete", g, AuthRequired, c, c.Users.MeAvatarDelete)
	post(me, "/me/email/request-change", "me.email.request_change", g, AuthRequired, c, c.Users.MeEmailRequestChange)
	post(me, "/me/email/confirm-change", "me.email.confirm_change", g, AuthRequired, c, c.Users.MeEmailConfirmChange)

	// Owners and admins may read private profiles, so the public profile accepts an optional token.
	optional := v1.Group("")
	optional.Use(auth.Optional...)
	get(optional, "/users/:param1", "public.users.profile", GroupPublic, AuthOptional, c, c.Users.PublicProfile)
}
