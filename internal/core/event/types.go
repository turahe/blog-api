package event

// Event types. Names match the channels in docs/architecture/asyncapi.yaml.
const (
	PostCreated   = "blog.post.created"
	PostUpdated   = "blog.post.updated"
	PostPublished = "blog.post.published"
	PostArchived  = "blog.post.archived"

	PostRevisionCreated  = "blog.post.revision.created"
	PostRevisionRestored = "blog.post.revision.restored"
	PostSEOUpdated       = "blog.post.seo.updated"
	PostSlugChanged      = "blog.post.slug_changed"

	AnalyticsConsentGranted   = "analytics.consent.granted"
	AnalyticsConsentRejected  = "analytics.consent.rejected"
	AnalyticsConsentWithdrawn = "analytics.consent.withdrawn"

	CommentCreated   = "blog.comment.created"
	CommentModerated = "blog.comment.moderated"

	UserCreated                = "blog.user.created"
	UserPasswordResetRequested = "auth.password.reset_requested"
	UserPrivacyUpdated         = "user.privacy.updated"
	UserExportRequested        = "user.activity.export_requested"
	UserErasureRequested       = "user.activity.erasure_requested"
	MediaUploaded              = "blog.media.uploaded"
	MediaDeleted               = "blog.media.deleted"
	NotificationEmailRequested = "notification.email.requested"

	SettingsUpdated = "blog.settings.updated"

	ImpersonationStarted = "blog.impersonation.started"
	ImpersonationExited  = "blog.impersonation.exited_manually"
	ImpersonationExpired = "blog.impersonation.expired"
	ImpersonationRevoked = "blog.impersonation.revoked_by_policy"

	NewsletterIssueSendRequested = "blog.newsletter.issue.send_requested"
	NewsletterSubscriberChanged  = "blog.newsletter.subscriber.changed"
)

// Aggregate types.
const (
	AggregatePost     = "post"
	AggregateComment  = "comment"
	AggregateUser     = "user"
	AggregateMedia    = "media"
	AggregateEmail    = "email"
	AggregateSettings = "settings"
	AggregateConsent  = "consent"

	AggregateImpersonation        = "impersonation_session"
	AggregateNewsletterIssue      = "newsletter_issue"
	AggregateNewsletterSubscriber = "newsletter_subscriber"
)
