package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterMe binds authenticated self-service routes under /api/v1.
func RegisterMe(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupSelfService
	me := v1.Group("")
	me.Use(auth.Required...)

	get(me, "/me", "me.get", g, AuthRequired, c, c.Users.MeGet)
	put(me, "/me/password", "me.password.update", g, AuthRequired, c, c.Auth.MePasswordUpdate)
}
