package routes

import (
	"github.com/gin-gonic/gin"
)

// registerNewsletter binds public double opt-in, self-service, and admin newsletter routes.
func registerNewsletter(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	pub, n := GroupPublic, AuthNone
	nl := c.Newsletter

	none := v1.Group("")
	post(none, "/newsletter/subscribe", "public.newsletter.subscribe", pub, n, c, nl.Subscribe)
	post(none, "/newsletter/confirm", "public.newsletter.confirm", pub, n, c, nl.Confirm)
	post(none, "/newsletter/confirm/resend", "public.newsletter.confirm_resend", pub, n, c, nl.ConfirmResend)
	post(none, "/newsletter/unsubscribe", "public.newsletter.unsubscribe", pub, n, c, nl.Unsubscribe)
	get(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.get", pub, n, c, nl.PreferencesGet)
	patch(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.patch", pub, n, c, nl.PreferencesPatch)
	post(none, "/newsletter/webhooks/provider", "public.newsletter.provider_webhook", pub, n, c, nl.ProviderWebhook)

	self, req := GroupSelfService, AuthRequired
	me := v1.Group("")
	me.Use(auth.Required...)
	get(me, "/me/newsletter/subscriptions", "me.newsletter.subscriptions.list", self, req, c, nl.MeSubscriptions)
	post(me, "/me/newsletter/subscribe", "me.newsletter.subscribe", self, req, c, nl.MeSubscribe)
	post(me, "/me/newsletter/unsubscribe", "me.newsletter.unsubscribe", self, req, c, nl.MeUnsubscribe)

	ag, ar := GroupAdmin, AuthRequired
	admin := v1.Group("/admin")
	admin.Use(auth.Required...)
	get(admin, "/newsletter/subscribers", "admin.newsletter.subscribers.list", ag, ar, c, nl.AdminSubscribersList)
	get(admin, "/newsletter/subscribers/:param1", "admin.newsletter.subscribers.get", ag, ar, c, nl.AdminSubscriberGet)
	del(admin, "/newsletter/subscribers/:param1", "admin.newsletter.subscribers.delete", ag, ar, c, nl.AdminSubscriberDelete)
	get(admin, "/newsletter/issues", "admin.newsletter.issues.list", ag, ar, c, nl.AdminIssuesList)
	post(admin, "/newsletter/issues", "admin.newsletter.issues.send", ag, ar, c, nl.AdminIssueCreate)
	get(admin, "/newsletter/issues/:param1", "admin.newsletter.issues.get", ag, ar, c, nl.AdminIssueGet)
	patch(admin, "/newsletter/issues/:param1", "admin.newsletter.issues.patch", ag, ar, c, nl.AdminIssuePatch)
	get(admin, "/newsletter/provider-config", "admin.newsletter.provider_config.get", ag, ar, c, nl.AdminConfigGet)
	put(admin, "/newsletter/provider-config", "admin.newsletter.provider_config.put", ag, ar, c, nl.AdminConfigPut)
}
