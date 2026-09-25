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
	MediaUploaded              = "blog.media.uploaded"
	MediaDeleted               = "blog.media.deleted"
	NotificationEmailRequested = "notification.email.requested"

	SettingsUpdated = "blog.settings.updated"
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
)
