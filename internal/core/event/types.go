package event

// Event types. Names match the channels in docs/architecture/asyncapi.yaml.
const (
	PostCreated   = "blog.post.created"
	PostUpdated   = "blog.post.updated"
	PostPublished = "blog.post.published"
	PostArchived  = "blog.post.archived"

	CommentCreated   = "blog.comment.created"
	CommentModerated = "blog.comment.moderated"

	UserCreated                = "blog.user.created"
	UserPasswordResetRequested = "auth.password.reset_requested"
	MediaUploaded              = "blog.media.uploaded"
	MediaDeleted               = "blog.media.deleted"
	NotificationEmailRequested = "notification.email.requested"
)

// Aggregate types.
const (
	AggregatePost    = "post"
	AggregateComment = "comment"
	AggregateUser    = "user"
	AggregateMedia   = "media"
)
