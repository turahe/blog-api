package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
	"github.com/turahe/blog-api/internal/core/notification/ports"
	"github.com/turahe/blog-api/internal/core/notification/template"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type memoryInbox struct {
	rows       []notificationdomain.Notification
	insertErr  error
	listErr    error
	listFilter notificationdomain.ListFilter
}

func (m *memoryInbox) Insert(_ context.Context, n notificationdomain.Notification) (notificationdomain.Notification, bool, error) {
	if m.insertErr != nil {
		return notificationdomain.Notification{}, false, m.insertErr
	}

	for _, row := range m.rows {
		if n.DedupeKey != "" && row.UserUUID == n.UserUUID && row.DedupeKey == n.DedupeKey {
			return notificationdomain.Notification{}, false, nil
		}
	}

	n.ID, n.UUID = int64(len(m.rows)+1), uuid.New()
	m.rows = append(m.rows, n)

	return n, true, nil
}

func (m *memoryInbox) List(_ context.Context, filter notificationdomain.ListFilter) (notificationdomain.ListResult, error) {
	if m.listErr != nil {
		return notificationdomain.ListResult{}, m.listErr
	}

	m.listFilter = filter

	return notificationdomain.ListResult{Total: int64(len(m.rows))}, nil
}

func (m *memoryInbox) MarkRead(_ context.Context, userID, id uuid.UUID, at time.Time) (notificationdomain.Notification, error) {
	for i, row := range m.rows {
		if row.UUID == id && row.UserUUID == userID {
			if row.ReadAt == nil {
				m.rows[i].ReadAt = &at
			}

			return m.rows[i], nil
		}
	}

	return notificationdomain.Notification{}, notificationdomain.ErrNotFound
}

func (m *memoryInbox) forUser(userID uuid.UUID) []notificationdomain.Notification {
	var out []notificationdomain.Notification

	for _, row := range m.rows {
		if row.UserUUID == userID {
			out = append(out, row)
		}
	}

	return out
}

type fakeDirectory struct {
	names         map[uuid.UUID]string
	posts         map[uuid.UUID]ports.PostSummary
	commenters    []uuid.UUID
	commentersErr error
}

func (d fakeDirectory) DisplayName(_ context.Context, userID uuid.UUID) (string, error) {
	name, ok := d.names[userID]
	if !ok {
		return "", notificationdomain.ErrNotFound
	}

	return name, nil
}

func (d fakeDirectory) PostSummary(_ context.Context, postID uuid.UUID) (ports.PostSummary, error) {
	post, ok := d.posts[postID]
	if !ok {
		return ports.PostSummary{}, notificationdomain.ErrNotFound
	}

	return post, nil
}

func (d fakeDirectory) PostCommenters(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return d.commenters, d.commentersErr
}

type inboxTemplates struct {
	rows map[string]template.Template
}

func (s inboxTemplates) Get(_ context.Context, typ string, channel template.Channel) (template.Template, error) {
	tpl, ok := s.rows[typ+"/"+string(channel)]
	if !ok {
		return template.Template{}, template.ErrNotFound
	}

	return tpl, nil
}

func (s inboxTemplates) List(context.Context) ([]template.Template, error)       { return nil, nil }
func (s inboxTemplates) Save(context.Context, template.Template) error           { return nil }
func (s inboxTemplates) SeedDefaults(context.Context, []template.Template) error { return nil }

type inboxFixture struct {
	inbox   *Inbox
	repo    *memoryInbox
	dir     fakeDirectory
	post    ports.PostSummary
	author  uuid.UUID
	replier uuid.UUID
}

func newInboxFixture() inboxFixture {
	author, replier := uuid.New(), uuid.New()
	post := ports.PostSummary{UUID: uuid.New(), AuthorUUID: author, Title: "Hello world", Slug: "hello-world"}
	repo := &memoryInbox{}
	dir := fakeDirectory{
		names: map[uuid.UUID]string{replier: "Grace"},
		posts: map[uuid.UUID]ports.PostSummary{post.UUID: post},
	}
	clock := fixedClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}

	return inboxFixture{
		inbox: NewInbox(repo, dir, clock, nil, "https://api.example.test/"),
		repo:  repo, dir: dir, post: post, author: author, replier: replier,
	}
}

func TestInboxWithTemplates(t *testing.T) {
	t.Parallel()

	reply := func(channel template.Channel, title string) template.Template {
		return template.Template{
			Type: template.TypeCommentReply, Channel: channel, Title: title, Body: "{{.Excerpt}}",
			Preview: "{{.Excerpt}}", Event: "notification.created",
		}
	}

	tests := []struct {
		name      string
		rows      []template.Template
		wantTitle string
		wantErr   string
	}{
		{
			name:      "stored copy is used",
			rows:      []template.Template{reply(template.ChannelWeb, "{{.ActorName}} answered you")},
			wantTitle: "Grace answered you",
		},
		{
			name:    "a broken web template stores nothing",
			rows:    []template.Template{reply(template.ChannelWeb, "{{.Missing}}")},
			wantErr: "render " + template.TypeCommentReply,
		},
		{
			name:    "a broken sse template stores nothing",
			rows:    []template.Template{reply(template.ChannelSSE, "{{.Missing}}")},
			wantErr: "render " + template.TypeCommentReply,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInboxFixture()
			store := inboxTemplates{rows: map[string]template.Template{}}

			for _, row := range tt.rows {
				store.rows[row.Type+"/"+string(row.Channel)] = row
			}

			f.inbox.WithTemplates(store)

			parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
			reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier, Content: "hi"}

			err := f.inbox.DeliverCommentReplied(t.Context(), reply, parent)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Empty(t, f.repo.rows)

				return
			}

			require.NoError(t, err)
			require.Len(t, f.repo.rows, 1)
			require.Equal(t, tt.wantTitle, f.repo.rows[0].Title)
		})
	}
}

func TestInboxList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		page        int
		perPage     int
		listErr     error
		wantPage    int
		wantPerPage int
	}{
		{name: "defaults", wantPage: 1, wantPerPage: defaultInboxPerPage},
		{name: "explicit page", page: 3, perPage: 10, wantPage: 3, wantPerPage: 10},
		{name: "store fails", listErr: errors.New("db down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInboxFixture()
			f.repo.listErr = tt.listErr

			got, err := f.inbox.List(t.Context(), f.author, true, tt.page, tt.perPage)
			if tt.listErr != nil {
				require.ErrorIs(t, err, tt.listErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantPage, got.Page)
			require.Equal(t, tt.wantPerPage, got.PerPage)
			require.Equal(t, notificationdomain.ListFilter{
				UserUUID: f.author, UnreadOnly: true, Page: tt.wantPage, PerPage: tt.wantPerPage,
			}, f.repo.listFilter)
		})
	}
}

func TestInboxCommentReplied(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
	reply := commentdomain.Comment{
		UUID: uuid.New(), PostUUID: f.post.UUID, ParentUUID: &parent.UUID, AuthorUUID: &f.replier,
		Content: "Great\npoint " + strings.Repeat("x", 200),
	}

	var pushed []notificationdomain.Notification

	f.inbox.OnCreated(func(_ context.Context, n notificationdomain.Notification) { pushed = append(pushed, n) })
	f.inbox.CommentReplied(context.Background(), reply, parent)
	f.inbox.CommentReplied(context.Background(), reply, parent)

	rows := f.repo.forUser(f.author)
	require.Len(t, rows, 1, "one notice per reply")
	require.Len(t, pushed, 1, "duplicates are not pushed")

	got := rows[0]
	require.Equal(t, template.TypeCommentReply, got.Type)
	require.Equal(t, "Grace replied to your comment", got.Title)
	require.True(t, strings.HasPrefix(got.Body, `On "Hello world": Great point x`), got.Body)
	require.True(t, strings.HasSuffix(got.Preview, "…"), "long replies are cut")
	require.Len(t, []rune(got.Preview), excerptRunes)
	require.Equal(t, &f.replier, got.ActorUUID)
	require.Equal(t, "https://api.example.test/api/v1/posts/hello-world", got.Payload["url"])
	require.Equal(t, reply.UUID.String(), got.Payload["comment_id"])
}

func TestInboxCommentRepliedSkips(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	guestParent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorName: "Guest"}
	ownParent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier}
	reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier, Content: "hi"}

	f.inbox.CommentReplied(context.Background(), reply, guestParent)
	f.inbox.CommentReplied(context.Background(), reply, ownParent)

	require.Empty(t, f.repo.rows, "guests have no inbox and self-replies are silent")
}

func TestInboxGuestReplyUsesGuestName(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
	reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorName: "Visitor", Content: "hi"}

	f.inbox.CommentReplied(context.Background(), reply, parent)

	rows := f.repo.forUser(f.author)
	require.Len(t, rows, 1)
	require.Equal(t, "Visitor replied to your comment", rows[0].Title)
	require.Nil(t, rows[0].ActorUUID)
}

func TestInboxCommentModerated(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	moderator := uuid.New()
	change := commentdomain.Moderation{
		Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier},
		From:    commentdomain.StatusApproved,
		Entry: commentdomain.ModerationEntry{
			UUID: uuid.New(), ModeratorUUID: &moderator, ToStatus: commentdomain.StatusSpam, Reason: "link farm",
		},
	}

	f.inbox.CommentModerated(context.Background(), change)

	rows := f.repo.forUser(f.replier)
	require.Len(t, rows, 1)
	require.Equal(t, "Your comment was marked as spam", rows[0].Title)
	require.Equal(t, `Your comment on "Hello world" was marked as spam. Reason: link farm`, rows[0].Body)
	require.Equal(t, "spam", rows[0].Payload["status"])

	change.Entry.UUID, change.Entry.Reason, change.Entry.ToStatus = uuid.New(), "", commentdomain.StatusApproved
	f.inbox.CommentModerated(context.Background(), change)

	rows = f.repo.forUser(f.replier)
	require.Len(t, rows, 2, "each decision is its own notice")
	require.Equal(t, `Your comment on "Hello world" was approved.`, rows[1].Body)
}

func TestInboxDeliverCommentModerated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		change    func(f inboxFixture) commentdomain.Moderation
		wantErr   error
		wantRows  int
		wantActor bool
	}{
		{
			name: "system decision has no actor",
			change: func(f inboxFixture) commentdomain.Moderation {
				return commentdomain.Moderation{
					Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier},
					Entry:   commentdomain.ModerationEntry{UUID: uuid.New(), ToStatus: commentdomain.StatusFlagged},
				}
			},
			wantRows: 1,
		},
		{
			name: "guest comments have no inbox",
			change: func(f inboxFixture) commentdomain.Moderation {
				return commentdomain.Moderation{
					Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID},
					Entry:   commentdomain.ModerationEntry{UUID: uuid.New(), ToStatus: commentdomain.StatusApproved},
				}
			},
		},
		{
			name: "moderating one's own comment is silent",
			change: func(f inboxFixture) commentdomain.Moderation {
				return commentdomain.Moderation{
					Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier},
					Entry:   commentdomain.ModerationEntry{UUID: uuid.New(), ModeratorUUID: &f.replier, ToStatus: commentdomain.StatusDeleted},
				}
			},
		},
		{
			name: "missing post",
			change: func(f inboxFixture) commentdomain.Moderation {
				return commentdomain.Moderation{
					Comment: commentdomain.Comment{UUID: uuid.New(), PostUUID: uuid.New(), AuthorUUID: &f.replier},
					Entry:   commentdomain.ModerationEntry{UUID: uuid.New(), ToStatus: commentdomain.StatusApproved},
				}
			},
			wantErr: notificationdomain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInboxFixture()

			err := f.inbox.DeliverCommentModerated(t.Context(), tt.change(f))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorContains(t, err, "post summary")

				return
			}

			require.NoError(t, err)
			require.Len(t, f.repo.rows, tt.wantRows)

			for _, row := range f.repo.rows {
				require.Nil(t, row.ActorUUID)
				require.Equal(t, "Your comment was held for review", row.Title)
			}
		})
	}
}

func TestInboxPostPublished(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		actor       func(f inboxFixture) *uuid.UUID
		wantAuthor  bool
		wantReplier bool
	}{
		"author publishes":    {actor: func(f inboxFixture) *uuid.UUID { return &f.author }, wantReplier: true},
		"editor publishes":    {actor: func(inboxFixture) *uuid.UUID { return new(uuid.New()) }, wantAuthor: true, wantReplier: true},
		"commenter publishes": {actor: func(f inboxFixture) *uuid.UUID { return &f.replier }, wantAuthor: true},
		"system publishes":    {actor: func(inboxFixture) *uuid.UUID { return nil }, wantAuthor: true, wantReplier: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newInboxFixture()
			f.dir.commenters = []uuid.UUID{f.author, f.replier}
			f.inbox.dir = f.dir
			post := postdomain.Post{
				UUID: f.post.UUID, AuthorUUID: f.author, Title: f.post.Title, Slug: f.post.Slug, PublishedAt: &published,
			}

			f.inbox.PostPublished(context.Background(), post, tc.actor(f))
			f.inbox.PostPublished(context.Background(), post, tc.actor(f))

			authorRows, replierRows := f.repo.forUser(f.author), f.repo.forUser(f.replier)
			require.Len(t, authorRows, boolCount(tc.wantAuthor))
			require.Len(t, replierRows, boolCount(tc.wantReplier))

			if tc.wantAuthor {
				require.Equal(t, template.TypePublicationPublished, authorRows[0].Type)
			}

			if tc.wantReplier {
				require.Equal(t, template.TypePublicationRepublished, replierRows[0].Type)
				require.Equal(t, `"Hello world" is published again.`, replierRows[0].Body)
			}
		})
	}
}

func TestInboxStoreFailureIsSwallowed(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	f.repo.insertErr = errors.New("db down")
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
	reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier, Content: "hi"}

	require.NotPanics(t, func() { f.inbox.CommentReplied(context.Background(), reply, parent) })
	require.Empty(t, f.repo.rows)
}

func TestInboxDeliverReturnsFailuresForRetry(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	f.repo.insertErr = errors.New("db down")
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
	reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier, Content: "hi"}

	require.ErrorIs(t, f.inbox.DeliverCommentReplied(t.Context(), reply, parent), f.repo.insertErr)

	missing := reply
	missing.PostUUID = uuid.New()
	require.ErrorIs(t, f.inbox.DeliverCommentReplied(t.Context(), missing, parent), notificationdomain.ErrNotFound)

	stranger := reply
	stranger.AuthorUUID = new(uuid.New())
	err := f.inbox.DeliverCommentReplied(t.Context(), stranger, parent)
	require.ErrorIs(t, err, notificationdomain.ErrNotFound)
	require.ErrorContains(t, err, "display name")

	f.repo.insertErr = nil
	require.NoError(t, f.inbox.DeliverCommentReplied(t.Context(), reply, parent))
	require.NoError(t, f.inbox.DeliverCommentReplied(t.Context(), reply, parent), "a redelivery is a no-op")
	require.Len(t, f.repo.forUser(f.author), 1)
}

func TestInboxDeliverPostPublishedReachesEveryRecipientDespiteFailures(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	f.repo.insertErr = errors.New("db down")
	f.dir.commenters = []uuid.UUID{f.replier}
	f.inbox.dir = f.dir
	post := postdomain.Post{UUID: f.post.UUID, AuthorUUID: f.author, Title: f.post.Title, Slug: f.post.Slug}

	err := f.inbox.DeliverPostPublished(t.Context(), post, nil)
	require.ErrorIs(t, err, f.repo.insertErr)
	require.Contains(t, err.Error(), template.TypePublicationPublished)
	require.Contains(t, err.Error(), template.TypePublicationRepublished)
}

func TestInboxDeliverPostPublishedCommentersUnavailable(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	f.dir.commentersErr = errors.New("directory down")
	f.inbox.dir = f.dir
	post := postdomain.Post{UUID: f.post.UUID, AuthorUUID: f.author, Title: f.post.Title, Slug: f.post.Slug}

	err := f.inbox.DeliverPostPublished(t.Context(), post, nil)
	require.ErrorIs(t, err, f.dir.commentersErr)
	require.ErrorContains(t, err, "post commenters")
	require.Len(t, f.repo.forUser(f.author), 1, "the author is still told")
}

func TestInboxListAndMarkRead(t *testing.T) {
	t.Parallel()

	f := newInboxFixture()
	parent := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.author}
	reply := commentdomain.Comment{UUID: uuid.New(), PostUUID: f.post.UUID, AuthorUUID: &f.replier, Content: "hi"}
	f.inbox.CommentReplied(context.Background(), reply, parent)

	result, err := f.inbox.List(context.Background(), f.author, false, 0, 500)
	require.NoError(t, err)
	require.Equal(t, 1, result.Page)
	require.Equal(t, maxInboxPerPage, result.PerPage)

	id := f.repo.rows[0].UUID

	_, err = f.inbox.MarkRead(context.Background(), f.replier, id)
	require.ErrorIs(t, err, notificationdomain.ErrNotFound, "only the recipient can mark it read")

	got, err := f.inbox.MarkRead(context.Background(), f.author, id)
	require.NoError(t, err)
	require.True(t, got.Read())
}

func TestOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status commentdomain.Status
		want   string
	}{
		{name: "approved", status: commentdomain.StatusApproved, want: "approved"},
		{name: "rejected", status: commentdomain.StatusRejected, want: "rejected"},
		{name: "spam", status: commentdomain.StatusSpam, want: "marked as spam"},
		{name: "deleted", status: commentdomain.StatusDeleted, want: "removed"},
		{name: "pending", status: commentdomain.StatusPending, want: "held for review"},
		{name: "flagged", status: commentdomain.StatusFlagged, want: "held for review"},
		{name: "unknown", status: "archived", want: "updated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, outcome(tt.status))
		})
	}
}

func boolCount(ok bool) int {
	if ok {
		return 1
	}

	return 0
}
