package middleware

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
)

// ErrorCodeImpersonationForbidden refuses an operation an impersonation token may not perform.
const ErrorCodeImpersonationForbidden = "impersonation.forbidden_action"

// HeaderImpersonationSession carries the session id on every response to an impersonation
// token, so clients can show that they act as someone else.
const HeaderImpersonationSession = "X-Impersonation-Session"

// contextImpersonationRefused marks a request the guard refused, so Audit records the attempt.
const contextImpersonationRefused = "impersonation_refused"

// impersonationBlocked lists operations that change the target's credentials, security,
// privacy, consent, or sessions, or that would chain impersonations. Consent must come from
// the person it belongs to. An impersonator acts as the target everywhere else.
var impersonationBlocked = map[string]bool{
	"me.newsletter.subscribe":    true,
	"me.newsletter.unsubscribe":  true,
	"analytics.consent.store":    true,
	"analytics.consent.withdraw": true,
	"me.password.update":         true,
	"me.email.request_change":    true,
	"me.email.confirm_change":    true,
	"me.2fa.get":                 true,
	"me.2fa.setup":               true,
	"me.2fa.confirm":             true,
	"me.2fa.disable":             true,
	"me.2fa.backup_codes":        true,
	"me.privacy.update":          true,
	"me.activity.export":         true,
	"me.activity.erase":          true,
	"auth.logout":                true,
	"auth.refresh":               true,
	"auth.oauth.start":           true,
	"auth.oauth.callback":        true,
	"admin.impersonation.start":  true,
}

// ImpersonationBlocked reports whether an impersonation token may not call operationID.
func ImpersonationBlocked(operationID string) bool {
	return impersonationBlocked[operationID]
}

// ImpersonationGuard refuses blocked operations to impersonation tokens with 403. It must run
// after routes.WithMeta and after authentication.
func ImpersonationGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := CurrentImpersonation(c); !ok {
			return
		}

		route, ok := routes.RouteOf(c)
		if ok && ImpersonationBlocked(route.OperationID) {
			c.Set(contextImpersonationRefused, true)
			responses.Failure(c, nethttp.StatusForbidden, ErrorCodeImpersonationForbidden,
				"This action is not available while impersonating")
		}
	}
}
