package service

import (
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

// postPayload is PostEventPayload in docs/architecture/asyncapi.yaml.
type postPayload struct {
	PostID      uuid.UUID  `json:"post_id"`
	AuthorID    uuid.UUID  `json:"author_id"`
	Status      string     `json:"status"`
	Slug        string     `json:"slug"`
	CategoryID  *uuid.UUID `json:"category_id"`
	Version     int64      `json:"version"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

func postEvent(typ string, post postdomain.Post, actorID *uuid.UUID, at time.Time) event.Event {
	return event.New(typ, event.AggregatePost, post.UUID, actorID, at, postPayload{
		PostID: post.UUID, AuthorID: post.AuthorUUID, Status: string(post.Status), Slug: post.Slug,
		CategoryID: post.CategoryUUID, Version: post.Version, PublishedAt: post.PublishedAt,
	})
}

// transitionEvent names the event for a post entering status.
func transitionEvent(status postdomain.Status) string {
	switch status {
	case postdomain.StatusPublished:
		return event.PostPublished
	case postdomain.StatusArchived:
		return event.PostArchived
	case postdomain.StatusDraft, postdomain.StatusScheduled:
		return event.PostUpdated
	}

	return event.PostUpdated
}
