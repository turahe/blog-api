package routes

import (
	"github.com/gin-gonic/gin"
)

// registerContractStubs mounts OpenAPI operations that are not yet implemented.
func registerContractStubs(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	none := v1.Group("")
	required := v1.Group("")
	required.Use(auth.Required...)

	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	pub, n := GroupPublic, AuthNone
	self, req := GroupSelfService, AuthRequired
	ag, ar := GroupAdmin, AuthRequired

	get(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.get", pub, n, c, nil)
	get(none, "/posts/:param1/seo-meta", "public.posts.seo_meta", pub, n, c, nil)
	patch(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.patch", pub, n, c, nil)
	post(none, "/newsletter/confirm", "public.newsletter.confirm", pub, n, c, nil)
	post(none, "/newsletter/confirm/resend", "public.newsletter.confirm_resend", pub, n, c, nil)
	post(none, "/newsletter/subscribe", "public.newsletter.subscribe", pub, n, c, nil)
	post(none, "/newsletter/unsubscribe", "public.newsletter.unsubscribe", pub, n, c, nil)

	get(required, "/me/activity/export", "me.activity.export", self, req, c, nil)
	get(required, "/me/newsletter/subscriptions", "me.newsletter.subscriptions.list", self, req, c, nil)
	get(required, "/me/privacy", "me.privacy.get", self, req, c, nil)
	post(required, "/me/activity/erase", "me.activity.erase", self, req, c, nil)
	post(required, "/me/newsletter/subscribe", "me.newsletter.subscribe", self, req, c, nil)
	post(required, "/me/newsletter/unsubscribe", "me.newsletter.unsubscribe", self, req, c, nil)
	put(required, "/me/privacy", "me.privacy.update", self, req, c, nil)

	get(admin, "/analytics/navigation", "admin.analytics.navigation", ag, ar, c, nil)
	get(admin, "/analytics/overview", "admin.analytics.overview", ag, ar, c, nil)
	get(admin, "/analytics/pages", "admin.analytics.pages", ag, ar, c, nil)
	get(admin, "/analytics/realtime/stream", "admin.analytics.realtime.stream", ag, ar, c, nil)
	get(admin, "/analytics/retention", "admin.analytics.retention", ag, ar, c, nil)
	get(admin, "/analytics/search", "admin.analytics.search", ag, ar, c, nil)
	post(admin, "/analytics/export", "admin.analytics.export", ag, ar, c, nil)

	get(admin, "/impersonation/current", "admin.impersonation.current", ag, ar, c, nil)
	post(admin, "/impersonation/start", "admin.impersonation.start", ag, ar, c, nil)
	post(admin, "/impersonation/stop", "admin.impersonation.stop", ag, ar, c, nil)

	get(admin, "/newsletter/issues", "admin.newsletter.issues.list", ag, ar, c, nil)
	get(admin, "/newsletter/issues/:param1", "admin.newsletter.issues.get", ag, ar, c, nil)
	get(admin, "/newsletter/provider-config", "admin.newsletter.provider_config.get", ag, ar, c, nil)
	get(admin, "/newsletter/subscribers", "admin.newsletter.subscribers.list", ag, ar, c, nil)
	get(admin, "/newsletter/subscribers/:param1", "admin.newsletter.subscribers.get", ag, ar, c, nil)
	del(admin, "/newsletter/subscribers/:param1", "admin.newsletter.subscribers.delete", ag, ar, c, nil)
	patch(admin, "/newsletter/issues/:param1", "admin.newsletter.issues.patch", ag, ar, c, nil)
	post(admin, "/newsletter/issues", "admin.newsletter.issues.send", ag, ar, c, nil)
	put(admin, "/newsletter/provider-config", "admin.newsletter.provider_config.put", ag, ar, c, nil)

	get(admin, "/posts/:param1/seo", "admin.posts.seo.get", ag, ar, c, nil)
	post(admin, "/posts/:param1/seo/preview", "admin.posts.seo.preview", ag, ar, c, nil)
	put(admin, "/posts/:param1/seo", "admin.posts.seo.update", ag, ar, c, nil)
}
