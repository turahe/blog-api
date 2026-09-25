package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentports "github.com/turahe/blog-api/internal/core/comment/ports"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/core/notification/template"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postports "github.com/turahe/blog-api/internal/core/post/ports"
)

const (
	defaultInboxPerPage = 20
	maxInboxPerPage     = 100
	excerptRunes        = 140
)

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Inbox stores in-app notifications and serves a user's inbox. It implements the comment
// and post notifiers; delivery failures are logged and never fail the caller.
type Inbox struct {
	repo      ports.Repository
	dir       ports.Directory
	renderer  *template.Renderer
	clock     Clock
	logger    *slog.Logger
	publicURL string
	listeners []func(context.Context, notificationdomain.Notification)
}

var (
	_ commentports.Notifier     = (*Inbox)(nil)
	_ postports.PublishNotifier = (*Inbox)(nil)
)

// NewInbox returns an Inbox rendering built-in copy until WithTemplates sets a store.
// publicURL is the API origin used for links.
func NewInbox(repo ports.Repository, dir ports.Directory, clock Clock, logger *slog.Logger, publicURL string) *Inbox {
	if logger == nil {
		logger = slog.Default()
	}

	return &Inbox{
		repo:      repo,
		dir:       dir,
		renderer:  template.NewRenderer(nil, logger),
		clock:     clock,
		logger:    logger,
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
	}
}

// WithTemplates renders copy from store, falling back to the built-in catalogue.
func (i *Inbox) WithTemplates(store template.Store) *Inbox {
	i.renderer = template.NewRenderer(store, i.logger)
	return i
}

// OnCreated registers fn to run after each newly stored notification, such as a live push.
func (i *Inbox) OnCreated(fn func(context.Context, notificationdomain.Notification)) *Inbox {
	i.listeners = append(i.listeners, fn)
	return i
}

// List returns a page of the user's notifications, newest first.
func (i *Inbox) List(ctx context.Context, userID uuid.UUID, unreadOnly bool, page, perPage int) (notificationdomain.ListResult, error) {
	if page < 1 {
		page = 1
	}

	if perPage < 1 {
		perPage = defaultInboxPerPage
	}

	perPage = min(perPage, maxInboxPerPage)

	result, err := i.repo.List(ctx, notificationdomain.ListFilter{
		UserUUID: userID, UnreadOnly: unreadOnly, Page: page, PerPage: perPage,
	})
	if err != nil {
		return notificationdomain.ListResult{}, err
	}

	result.Page, result.PerPage = page, perPage

	return result, nil
}

// MarkRead marks one of the user's notifications read; repeating it keeps the first read time.
func (i *Inbox) MarkRead(ctx context.Context, userID, id uuid.UUID) (notificationdomain.Notification, error) {
	return i.repo.MarkRead(ctx, userID, id, i.clock.Now())
}

// CommentReplied tells the parent's registered author about a visible reply by someone else.
func (i *Inbox) CommentReplied(ctx context.Context, reply, parent commentdomain.Comment) {
	if parent.AuthorUUID == nil || sameUser(reply.AuthorUUID, *parent.AuthorUUID) {
		return
	}

	ctx = context.WithoutCancel(ctx)

	post, err := i.dir.PostSummary(ctx, reply.PostUUID)
	if err != nil {
		i.logFailure(ctx, template.TypeCommentReply, err)
		return
	}

	actorName := reply.AuthorName
	if reply.AuthorUUID != nil {
		if actorName, err = i.dir.DisplayName(ctx, *reply.AuthorUUID); err != nil {
			i.logFailure(ctx, template.TypeCommentReply, err)
			return
		}
	}

	i.deliver(ctx, notice{
		userID: *parent.AuthorUUID,
		typ:    template.TypeCommentReply,
		data: template.Data{
			ActorName: actorName, PostTitle: post.Title, PostURL: i.postURL(post),
			Excerpt: excerpt(reply.Content),
		},
		payload: map[string]string{
			"post_id": post.UUID.String(), "comment_id": reply.UUID.String(),
			"parent_id": parent.UUID.String(), "url": i.postURL(post),
		},
		actorID: reply.AuthorUUID,
		dedupe:  template.TypeCommentReply + ":" + reply.UUID.String(),
	})
}

// CommentModerated tells a registered author the outcome of a moderator's decision.
func (i *Inbox) CommentModerated(ctx context.Context, change commentdomain.Moderation) {
	author := change.Comment.AuthorUUID
	if author == nil || sameUser(change.Entry.ModeratorUUID, *author) {
		return
	}

	ctx = context.WithoutCancel(ctx)

	post, err := i.dir.PostSummary(ctx, change.Comment.PostUUID)
	if err != nil {
		i.logFailure(ctx, template.TypeCommentModerated, err)
		return
	}

	i.deliver(ctx, notice{
		userID: *author,
		typ:    template.TypeCommentModerated,
		data: template.Data{
			PostTitle: post.Title, PostURL: i.postURL(post),
			Outcome: outcome(change.Entry.ToStatus), Reason: change.Entry.Reason,
		},
		payload: map[string]string{
			"post_id": post.UUID.String(), "comment_id": change.Comment.UUID.String(),
			"status": string(change.Entry.ToStatus), "url": i.postURL(post),
		},
		actorID: change.Entry.ModeratorUUID,
		dedupe:  template.TypeCommentModerated + ":" + change.Entry.UUID.String(),
	})
}

// PostPublished tells the author when someone else published their post, and tells the
// registered commenters that the post they discussed is public again.
func (i *Inbox) PostPublished(ctx context.Context, post postdomain.Post, actorID *uuid.UUID) {
	ctx = context.WithoutCancel(ctx)
	summary := ports.PostSummary{UUID: post.UUID, AuthorUUID: post.AuthorUUID, Title: post.Title, Slug: post.Slug}
	payload := map[string]string{"post_id": post.UUID.String(), "url": i.postURL(summary)}

	edition := post.UUID.String()
	if post.PublishedAt != nil {
		edition += ":" + strconv.FormatInt(post.PublishedAt.Unix(), 10)
	}

	data := template.Data{PostTitle: post.Title, PostURL: i.postURL(summary)}

	if !sameUser(actorID, post.AuthorUUID) {
		i.deliver(ctx, notice{
			userID: post.AuthorUUID, typ: template.TypePublicationPublished, data: data,
			payload: payload, actorID: actorID, dedupe: template.TypePublicationPublished + ":" + edition,
		})
	}

	commenters, err := i.dir.PostCommenters(ctx, post.UUID)
	if err != nil {
		i.logFailure(ctx, template.TypePublicationRepublished, err)
		return
	}

	for _, userID := range commenters {
		if userID == post.AuthorUUID || sameUser(actorID, userID) {
			continue
		}

		i.deliver(ctx, notice{
			userID: userID, typ: template.TypePublicationRepublished, data: data,
			payload: payload, actorID: actorID, dedupe: template.TypePublicationRepublished + ":" + edition,
		})
	}
}

type notice struct {
	userID  uuid.UUID
	typ     string
	data    template.Data
	payload map[string]string
	actorID *uuid.UUID
	dedupe  string
}

func (i *Inbox) deliver(ctx context.Context, n notice) {
	web, err := i.renderer.Render(ctx, template.ChannelWeb, n.typ, n.data)
	if err != nil {
		i.logFailure(ctx, n.typ, err)
		return
	}

	sse, err := i.renderer.Render(ctx, template.ChannelSSE, n.typ, n.data)
	if err != nil {
		i.logFailure(ctx, n.typ, err)
		return
	}

	stored, created, err := i.repo.Insert(ctx, notificationdomain.Notification{
		UserUUID:  n.userID,
		Type:      n.typ,
		Title:     web.Title,
		Body:      web.Body,
		Preview:   sse.Preview,
		Payload:   n.payload,
		ActorUUID: n.actorID,
		DedupeKey: n.dedupe,
		CreatedAt: i.clock.Now(),
	})
	if err != nil {
		i.logFailure(ctx, n.typ, err)
		return
	}

	if !created {
		return
	}

	for _, listener := range i.listeners {
		listener(ctx, stored)
	}
}

func (i *Inbox) logFailure(ctx context.Context, typ string, err error) {
	i.logger.WarnContext(ctx, "notify: in-app notification not stored", "type", typ, "error", err)
}

func (i *Inbox) postURL(post ports.PostSummary) string {
	return i.publicURL + "/api/v1/posts/" + post.Slug
}

func sameUser(actor *uuid.UUID, user uuid.UUID) bool {
	return actor != nil && *actor == user
}

func outcome(status commentdomain.Status) string {
	switch status {
	case commentdomain.StatusApproved:
		return "approved"
	case commentdomain.StatusRejected:
		return "rejected"
	case commentdomain.StatusSpam:
		return "marked as spam"
	case commentdomain.StatusDeleted:
		return "removed"
	case commentdomain.StatusPending, commentdomain.StatusFlagged:
		return "held for review"
	}

	return "updated"
}

// excerpt returns the first excerptRunes runes of text on one line, with an ellipsis when cut.
func excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= excerptRunes {
		return text
	}

	return string([]rune(text)[:excerptRunes-1]) + "…"
}
