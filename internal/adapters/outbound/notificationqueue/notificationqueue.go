// Package notificationqueue moves in-app notification delivery to app worker. The API records
// one notification.requested command in the outbox; the worker renders, stores, and pushes the
// notifications, so a publish that fans out to every commenter never runs inside a request.
package notificationqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentports "github.com/turahe/blog-api/internal/core/comment/ports"
	"github.com/turahe/blog-api/internal/core/event"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postports "github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/platform/messaging"
)

// Command kinds.
const (
	KindCommentReplied   = "comment_replied"
	KindCommentModerated = "comment_moderated"
	KindPostPublished    = "post_published"
)

// Command is the notification.requested payload. It carries only what the inbox renders from:
// no email addresses, IP hashes, or user agents.
type Command struct {
	Kind       string      `json:"kind"`
	Reply      *Comment    `json:"reply,omitempty"`
	Parent     *Comment    `json:"parent,omitempty"`
	Moderation *Moderation `json:"moderation,omitempty"`
	Post       *Post       `json:"post,omitempty"`
	ActorID    *uuid.UUID  `json:"actor_id,omitempty"`
}

// Comment is the rendered part of a comment.
type Comment struct {
	ID         uuid.UUID  `json:"id"`
	PostID     uuid.UUID  `json:"post_id"`
	AuthorID   *uuid.UUID `json:"author_id,omitempty"`
	AuthorName string     `json:"author_name,omitempty"`
	Content    string     `json:"content,omitempty"`
}

// Moderation is the rendered part of a moderation decision.
type Moderation struct {
	EntryID     uuid.UUID  `json:"entry_id"`
	CommentID   uuid.UUID  `json:"comment_id"`
	PostID      uuid.UUID  `json:"post_id"`
	AuthorID    *uuid.UUID `json:"author_id,omitempty"`
	ModeratorID *uuid.UUID `json:"moderator_id,omitempty"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
}

// Post is the rendered part of a published post.
type Post struct {
	ID          uuid.UUID  `json:"id"`
	AuthorID    uuid.UUID  `json:"author_id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// Inline delivers notifications in the calling process.
type Inline interface {
	commentports.Notifier
	postports.PublishNotifier
}

// Notifier implements the comment and post notifiers by recording commands. When a command
// cannot be stored the notification is delivered inline so it is not lost.
type Notifier struct {
	recorder event.Recorder
	inline   Inline
	logger   *slog.Logger
	now      func() time.Time
}

var (
	_ commentports.Notifier     = (*Notifier)(nil)
	_ postports.PublishNotifier = (*Notifier)(nil)
)

// New returns a Notifier that records commands with recorder and falls back to inline.
func New(recorder event.Recorder, inline Inline, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}

	return &Notifier{recorder: recorder, inline: inline, logger: logger, now: time.Now}
}

// CommentReplied queues a reply notice.
func (n *Notifier) CommentReplied(ctx context.Context, reply, parent commentdomain.Comment) {
	cmd := Command{Kind: KindCommentReplied, Reply: commentOf(reply), Parent: commentOf(parent)}
	if !n.enqueue(ctx, event.AggregateComment, reply.UUID, reply.AuthorUUID, cmd) {
		n.inline.CommentReplied(ctx, reply, parent)
	}
}

// CommentModerated queues a moderation notice.
func (n *Notifier) CommentModerated(ctx context.Context, change commentdomain.Moderation) {
	cmd := Command{Kind: KindCommentModerated, Moderation: &Moderation{
		EntryID: change.Entry.UUID, CommentID: change.Comment.UUID, PostID: change.Comment.PostUUID,
		AuthorID: change.Comment.AuthorUUID, ModeratorID: change.Entry.ModeratorUUID,
		Status: string(change.Entry.ToStatus), Reason: change.Entry.Reason,
	}}
	if !n.enqueue(ctx, event.AggregateComment, change.Comment.UUID, change.Entry.ModeratorUUID, cmd) {
		n.inline.CommentModerated(ctx, change)
	}
}

// PostPublished queues the publication notices.
func (n *Notifier) PostPublished(ctx context.Context, post postdomain.Post, actorID *uuid.UUID) {
	cmd := Command{Kind: KindPostPublished, ActorID: actorID, Post: &Post{
		ID: post.UUID, AuthorID: post.AuthorUUID, Title: post.Title, Slug: post.Slug, PublishedAt: post.PublishedAt,
	}}
	if !n.enqueue(ctx, event.AggregatePost, post.UUID, actorID, cmd) {
		n.inline.PostPublished(ctx, post, actorID)
	}
}

func (n *Notifier) enqueue(ctx context.Context, aggregate string, id uuid.UUID, actor *uuid.UUID, cmd Command) bool {
	ctx = context.WithoutCancel(ctx)

	err := n.recorder.Record(ctx, event.New(event.NotificationRequested, aggregate, id, actor, n.now(), cmd))
	if err != nil {
		n.logger.WarnContext(ctx, "notificationqueue: enqueue failed, delivering inline", "kind", cmd.Kind, "error", err)
		return false
	}

	return true
}

func commentOf(c commentdomain.Comment) *Comment {
	return &Comment{ID: c.UUID, PostID: c.PostUUID, AuthorID: c.AuthorUUID, AuthorName: c.AuthorName, Content: c.Content}
}

func (c Comment) domain() commentdomain.Comment {
	return commentdomain.Comment{
		UUID: c.ID, PostUUID: c.PostID, AuthorUUID: c.AuthorID, AuthorName: c.AuthorName, Content: c.Content,
	}
}

// Deliverer stores notifications and reports failures so the worker can retry. Repeating a
// delivery must not store a second notification.
type Deliverer interface {
	DeliverCommentReplied(ctx context.Context, reply, parent commentdomain.Comment) error
	DeliverCommentModerated(ctx context.Context, change commentdomain.Moderation) error
	DeliverPostPublished(ctx context.Context, post postdomain.Post, actorID *uuid.UUID) error
}

// ErrMalformed marks a command that can never be delivered; it is dead-lettered without retries.
var ErrMalformed = fmt.Errorf("malformed notification command: %w", messaging.ErrPermanent)

// Handler returns a worker handler that delivers each command through inbox.
func Handler(inbox Deliverer) message.NoPublishHandlerFunc {
	return func(msg *message.Message) error {
		var cmd Command
		if err := json.Unmarshal(msg.Payload, &cmd); err != nil {
			return fmt.Errorf("%w: payload is not a command", ErrMalformed)
		}

		ctx := msg.Context()

		var err error

		switch {
		case cmd.Kind == KindCommentReplied && cmd.Reply != nil && cmd.Parent != nil:
			err = inbox.DeliverCommentReplied(ctx, cmd.Reply.domain(), cmd.Parent.domain())
		case cmd.Kind == KindCommentModerated && cmd.Moderation != nil:
			m := cmd.Moderation
			err = inbox.DeliverCommentModerated(ctx, commentdomain.Moderation{
				Comment: commentdomain.Comment{UUID: m.CommentID, PostUUID: m.PostID, AuthorUUID: m.AuthorID},
				Entry: commentdomain.ModerationEntry{
					UUID: m.EntryID, CommentUUID: m.CommentID, ModeratorUUID: m.ModeratorID,
					ToStatus: commentdomain.Status(m.Status), Reason: m.Reason,
				},
			})
		case cmd.Kind == KindPostPublished && cmd.Post != nil:
			p := cmd.Post
			err = inbox.DeliverPostPublished(ctx, postdomain.Post{
				UUID: p.ID, AuthorUUID: p.AuthorID, Title: p.Title, Slug: p.Slug, PublishedAt: p.PublishedAt,
			}, cmd.ActorID)
		default:
			return fmt.Errorf("%w: kind %q without its fields", ErrMalformed, cmd.Kind)
		}

		if err != nil {
			return fmt.Errorf("deliver %s: %w", cmd.Kind, err)
		}

		return nil
	}
}
