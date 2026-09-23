package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterAuth binds /auth routes (mixed auth modes).
func RegisterAuth(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAuth

	none := v1.Group("")
	post(none, "/auth/login", "auth.login", g, AuthNone, c, c.Auth.Login)
	post(none, "/auth/refresh", "auth.refresh", g, AuthNone, c, c.Auth.Refresh)
	post(none, "/auth/password/forgot", "auth.password.forgot", g, AuthNone, c, c.Auth.PasswordForgot)
	get(none, "/auth/password/reset/:param1", "auth.password.reset_token_validity", g, AuthNone, c, c.Auth.PasswordResetValidity)
	post(none, "/auth/password/reset", "auth.password.reset", g, AuthNone, c, c.Auth.PasswordReset)

	required := v1.Group("")
	required.Use(auth.Required...)
	post(required, "/auth/logout", "auth.logout", g, AuthRequired, c, c.Auth.Logout)
}
