// Package ports defines the outbound contracts of the notification core: mail delivery,
// the in-app inbox store, and the lookups used to write notice copy.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

// Message is one plain-text email. Callers must not put secrets in Subject.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer delivers a single message. Implementations own retries and transport errors.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Repository stores in-app notifications.
type Repository interface {
	// Insert stores n and returns it with its id. When the user already has a row with the
	// same non-empty DedupeKey nothing is written and created is false.
	Insert(ctx context.Context, n notificationdomain.Notification) (stored notificationdomain.Notification, created bool, err error)
	List(ctx context.Context, filter notificationdomain.ListFilter) (notificationdomain.ListResult, error)
	// MarkRead sets read_at to at unless already set and returns the row. A notification that
	// does not exist or belongs to another user yields ErrNotFound.
	MarkRead(ctx context.Context, userID, id uuid.UUID, at time.Time) (notificationdomain.Notification, error)
}

// PostSummary is the post detail used in notice copy.
type PostSummary struct {
	UUID       uuid.UUID
	AuthorUUID uuid.UUID
	Title      string
	Slug       string
}

// Directory looks up the people and posts named in notices.
type Directory interface {
	// DisplayName returns the user's name, falling back to the username.
	DisplayName(ctx context.Context, userID uuid.UUID) (string, error)
	PostSummary(ctx context.Context, postID uuid.UUID) (PostSummary, error)
	// PostCommenters returns the registered users with an approved comment on the post.
	PostCommenters(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
}
