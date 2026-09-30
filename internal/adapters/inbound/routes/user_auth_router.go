package routes

import (
	"github.com/gin-gonic/gin"
)

// RegisterUserAuthRouter mounts the user-facing /auth routes. Anonymous routes
// (login, register, verify, refresh, password reset, oauth, and the 2fa challenge)
// run without middleware; /auth/logout requires a bearer token.
func RegisterUserAuthRouter(router *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	g := GroupAuth

	none := router.Group("")
	post(none, "/auth/login", "auth.login", g, AuthNone, c, c.Auth.Login)
	post(none, "/auth/register", "auth.register", g, AuthNone, c, c.Auth.Register)
	post(none, "/auth/verify-email", "auth.verify_email", g, AuthNone, c, c.Auth.VerifyEmail)
	post(none, "/auth/refresh", "auth.refresh", g, AuthNone, c, c.Auth.Refresh)
	post(none, "/auth/password/forgot", "auth.password.forgot", g, AuthNone, c, c.Auth.PasswordForgot)
	get(none, "/auth/password/reset/:param1", "auth.password.reset_token_validity", g, AuthNone, c, c.Auth.PasswordResetValidity)
	post(none, "/auth/password/reset", "auth.password.reset", g, AuthNone, c, c.Auth.PasswordReset)
	get(none, "/auth/oauth/:param1/start", "auth.oauth.start", g, AuthNone, c, c.Auth.OAuthStart)
	post(none, "/auth/oauth/:param1/callback", "auth.oauth.callback", g, AuthNone, c, c.Auth.OAuthCallback)
	post(none, "/auth/2fa/challenge", "auth.2fa.challenge", g, AuthNone, c, c.Auth.TwoFactorChallenge)

	required := router.Group("")
	required.Use(auth.Required...)
	post(required, "/auth/logout", "auth.logout", g, AuthRequired, c, c.Auth.Logout)
}
