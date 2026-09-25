package service

import (
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/event"
)

// commentPayload is CommentEventPayload in docs/architecture/asyncapi.yaml; moderation
// events add the action and the status the comment left.
type commentPayload struct {
	CommentID  uuid.UUID  `json:"comment_id"`
	PostID     uuid.UUID  `json:"post_id"`
	ParentID   *uuid.UUID `json:"parent_id"`
	AuthorID   *uuid.UUID `json:"author_id"`
	Status     string     `json:"status"`
	Action     string     `json:"action,omitempty"`
	FromStatus string     `json:"from_status,omitempty"`
}

func commentCreatedEvent(c commentdomain.Comment) event.Event {
	return event.New(event.CommentCreated, event.AggregateComment, c.UUID, c.AuthorUUID, c.CreatedAt, commentPayload{
		CommentID: c.UUID, PostID: c.PostUUID, ParentID: c.ParentUUID, AuthorID: c.AuthorUUID, Status: string(c.Status),
	})
}

func commentModeratedEvent(c commentdomain.Comment, entry commentdomain.ModerationEntry, at time.Time) event.Event {
	return event.New(event.CommentModerated, event.AggregateComment, c.UUID, entry.ModeratorUUID, at, commentPayload{
		CommentID: c.UUID, PostID: c.PostUUID, ParentID: c.ParentUUID, AuthorID: c.AuthorUUID,
		Status: string(entry.ToStatus), Action: string(entry.Action), FromStatus: string(entry.FromStatus),
	})
}

func moderationEvents(changes []commentdomain.Moderation) []event.Event {
	events := make([]event.Event, 0, len(changes))
	for _, change := range changes {
		events = append(events, commentModeratedEvent(change.Comment, change.Entry, change.Entry.CreatedAt))
	}

	return events
}
