package routes

import (
	"github.com/gin-gonic/gin"
)

// registerContractStubs mounts OpenAPI operations that are not yet implemented.
func registerContractStubs(v1 *gin.RouterGroup, auth AuthMiddleware, c Controllers) {
	none := v1.Group("")
	optional := v1.Group("")
	optional.Use(auth.Optional...)
	required := v1.Group("")
	required.Use(auth.Required...)
	admin := v1.Group("/admin")
	admin.Use(auth.Required...)

	pub, n := GroupPublic, AuthNone
	self, req := GroupSelfService, AuthRequired
	ag, ar := GroupAdmin, AuthRequired
	authG := GroupAuth

	get(none, "/comments/:param1", "public.comments.get", pub, n, c, nil)
	get(none, "/media/:param1/transform", "public.media.transform", pub, n, c, nil)
	get(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.get", pub, n, c, nil)
	get(none, "/posts/:param1/comments", "public.posts.comments.list", pub, n, c, nil)
	get(none, "/posts/:param1/seo-meta", "public.posts.seo_meta", pub, n, c, nil)
	get(none, "/users/:param1", "public.users.profile", pub, n, c, nil)
	patch(none, "/newsletter/preferences/:param1", "public.newsletter.preferences.patch", pub, n, c, nil)
	post(none, "/newsletter/confirm", "public.newsletter.confirm", pub, n, c, nil)
	post(none, "/newsletter/confirm/resend", "public.newsletter.confirm_resend", pub, n, c, nil)
	post(none, "/newsletter/subscribe", "public.newsletter.subscribe", pub, n, c, nil)
	post(none, "/newsletter/unsubscribe", "public.newsletter.unsubscribe", pub, n, c, nil)

	post(optional, "/comments/:param1/flag", "public.comments.flag", pub, AuthOptional, c, nil)
	post(optional, "/posts/:param1/comments", "public.posts.comments.create", pub, AuthOptional, c, nil)

	post(none, "/auth/oauth/:param1/callback", "auth.oauth.callback", authG, n, c, nil)
	post(required, "/auth/2fa/challenge", "auth.2fa.challenge", authG, req, c, nil)

	get(required, "/me/activity", "me.activity.list", self, req, c, nil)
	get(required, "/me/activity/export", "me.activity.export", self, req, c, nil)
	get(required, "/me/comments", "self.comments.list", self, req, c, nil)
	get(required, "/me/newsletter/subscriptions", "me.newsletter.subscriptions.list", self, req, c, nil)
	get(required, "/me/notifications", "me.notifications.list", self, req, c, nil)
	get(required, "/me/notifications/stream", "me.notifications.stream", self, req, c, nil)
	get(required, "/me/privacy", "me.privacy.get", self, req, c, nil)
	del(required, "/me/avatar", "me.avatar.delete", self, req, c, nil)
	del(required, "/comments/:param1", "self.comments.delete", self, req, c, nil)
	patch(required, "/comments/:param1", "self.comments.patch", self, req, c, nil)
	patch(required, "/me/profile", "me.profile.patch", self, req, c, nil)
	post(required, "/comments/:param1/upvote", "self.comments.upvote", self, req, c, nil)
	post(required, "/me/activity/erase", "me.activity.erase", self, req, c, nil)
	post(required, "/me/avatar", "me.avatar.upload", self, req, c, nil)
	post(required, "/me/email/confirm-change", "me.email.confirm_change", self, req, c, nil)
	post(required, "/me/email/request-change", "me.email.request_change", self, req, c, nil)
	post(required, "/me/newsletter/subscribe", "me.newsletter.subscribe", self, req, c, nil)
	post(required, "/me/newsletter/unsubscribe", "me.newsletter.unsubscribe", self, req, c, nil)
	post(required, "/me/notifications/:param1/read", "me.notifications.read", self, req, c, nil)
	put(required, "/me/privacy", "me.privacy.update", self, req, c, nil)

	get(admin, "/analytics/navigation", "admin.analytics.navigation", ag, ar, c, nil)
	get(admin, "/analytics/overview", "admin.analytics.overview", ag, ar, c, nil)
	get(admin, "/analytics/pages", "admin.analytics.pages", ag, ar, c, nil)
	get(admin, "/analytics/realtime/stream", "admin.analytics.realtime.stream", ag, ar, c, nil)
	get(admin, "/analytics/retention", "admin.analytics.retention", ag, ar, c, nil)
	get(admin, "/analytics/search", "admin.analytics.search", ag, ar, c, nil)
	post(admin, "/analytics/export", "admin.analytics.export", ag, ar, c, nil)

	get(admin, "/comments", "admin.comments.list", ag, ar, c, nil)
	get(admin, "/comments/:param1", "admin.comments.get", ag, ar, c, nil)
	get(admin, "/comments/stats", "admin.comments.stats", ag, ar, c, nil)
	del(admin, "/comments/:param1", "admin.comments.delete", ag, ar, c, nil)
	post(admin, "/comments/:param1/moderate", "admin.comments.moderate", ag, ar, c, nil)
	post(admin, "/comments/bulk-moderate", "admin.comments.bulk_moderate", ag, ar, c, nil)

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

	get(admin, "/posts/:param1/revisions", "admin.posts.revisions.list", ag, ar, c, nil)
	get(admin, "/posts/:param1/revisions/:param2", "admin.posts.revisions.get", ag, ar, c, nil)
	get(admin, "/posts/:param1/seo", "admin.posts.seo.get", ag, ar, c, nil)
	post(admin, "/posts/:param1/revisions/:param2/restore", "admin.posts.revisions.restore", ag, ar, c, nil)
	post(admin, "/posts/:param1/seo/preview", "admin.posts.seo.preview", ag, ar, c, nil)
	put(admin, "/posts/:param1/seo", "admin.posts.seo.update", ag, ar, c, nil)

	get(admin, "/settings", "admin.settings.get", ag, ar, c, nil)
	get(admin, "/settings/history", "admin.settings.history", ag, ar, c, nil)
	put(admin, "/settings", "admin.settings.put", ag, ar, c, nil)

	get(admin, "/users/:param1/activity", "admin.users.activity.list", ag, ar, c, nil)
	get(admin, "/users/:param1/profile", "admin.users.profile.get", ag, ar, c, nil)
	patch(admin, "/users/:param1/profile", "admin.users.profile.patch", ag, ar, c, nil)
	post(admin, "/users", "admin.users.create", ag, ar, c, nil)
	post(admin, "/users/:param1/password/admin-reset", "admin.users.password.admin_reset", ag, ar, c, nil)
}
