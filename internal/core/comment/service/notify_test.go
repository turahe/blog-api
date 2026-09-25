package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

type replyNotice struct{ reply, parent uuid.UUID }

type fakeNotifier struct {
	replies   []replyNotice
	moderated []commentdomain.Moderation
}

func (n *fakeNotifier) CommentReplied(_ context.Context, reply, parent commentdomain.Comment) {
	n.replies = append(n.replies, replyNotice{reply: reply.UUID, parent: parent.UUID})
}

func (n *fakeNotifier) CommentModerated(_ context.Context, change commentdomain.Moderation) {
	n.moderated = append(n.moderated, change)
}

func TestCreateReplyNotifiesWhenVisible(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		cfg    Config
		guest  bool
		notify bool
	}{
		"approved reply":          {notify: true},
		"reply awaiting approval": {cfg: Config{RequireApproval: true}},
		"guest reply is pending":  {cfg: Config{GuestEnabled: true}, guest: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			notifier := &fakeNotifier{}
			tc.cfg.Notifier = notifier
			f := newFixture(tc.cfg)
			parent := f.seed(commentdomain.Comment{AuthorUUID: new(uuid.New())})

			in := CreateInput{PostUUID: f.post, ParentUUID: &parent.UUID, AuthorUUID: &f.user, Content: "reply"}
			if tc.guest {
				in.AuthorUUID, in.AuthorName, in.AuthorEmail = nil, "Guest", "guest@example.test"
			}

			reply, err := f.svc.Create(context.Background(), in)
			require.NoError(t, err)

			if !tc.notify {
				require.Empty(t, notifier.replies)
				return
			}

			require.Equal(t, []replyNotice{{reply: reply.UUID, parent: parent.UUID}}, notifier.replies)
		})
	}
}

func TestCreateRootCommentDoesNotNotify(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	f := newFixture(Config{Notifier: notifier})

	_, err := f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "root"})
	require.NoError(t, err)
	require.Empty(t, notifier.replies)
}

func TestModerateNotifiesAuthorAndParent(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	f := newFixture(Config{Notifier: notifier})
	parent := f.seed(commentdomain.Comment{AuthorUUID: new(uuid.New())})
	reply := f.seed(commentdomain.Comment{
		ParentUUID: &parent.UUID, AuthorUUID: &f.user, Status: commentdomain.StatusPending, Depth: 1,
	})

	_, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: reply.UUID, Action: commentdomain.ActionApprove, NotifyAuthor: true,
	})
	require.NoError(t, err)

	require.Len(t, notifier.moderated, 1)
	require.Equal(t, commentdomain.StatusApproved, notifier.moderated[0].Entry.ToStatus)
	require.Equal(t, []replyNotice{{reply: reply.UUID, parent: parent.UUID}}, notifier.replies)
}

func TestModerateSkipsNoticesThatDoNotApply(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	f := newFixture(Config{Notifier: notifier})
	parent := f.seed(commentdomain.Comment{AuthorUUID: new(uuid.New())})
	flagged := f.seed(commentdomain.Comment{ParentUUID: &parent.UUID, AuthorUUID: &f.user, Status: commentdomain.StatusFlagged})
	guest := f.seed(commentdomain.Comment{AuthorName: "Guest", Status: commentdomain.StatusPending})

	_, err := f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: flagged.UUID, Action: commentdomain.ActionApprove,
	})
	require.NoError(t, err)

	_, err = f.svc.Moderate(context.Background(), ModerateInput{
		ModeratorUUID: uuid.New(), CommentUUID: guest.UUID, Action: commentdomain.ActionReject, NotifyAuthor: true,
	})
	require.NoError(t, err)

	require.Empty(t, notifier.replies, "a flagged reply was already visible")
	require.Empty(t, notifier.moderated, "no notice without notify_author or for guests")
}

func TestBulkModerateNotifiesApprovedReplies(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	f := newFixture(Config{Notifier: notifier})
	parent := f.seed(commentdomain.Comment{AuthorUUID: new(uuid.New())})
	first := f.seed(commentdomain.Comment{ParentUUID: &parent.UUID, AuthorUUID: &f.user, Status: commentdomain.StatusPending})
	second := f.seed(commentdomain.Comment{ParentUUID: &parent.UUID, AuthorUUID: &f.user, Status: commentdomain.StatusSpam})
	root := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Status: commentdomain.StatusPending})

	n, err := f.svc.BulkModerate(context.Background(), BulkModerateInput{
		ModeratorUUID: uuid.New(), CommentUUIDs: []uuid.UUID{first.UUID, second.UUID, root.UUID},
		Action: commentdomain.ActionApprove,
	})
	require.NoError(t, err)
	require.Equal(t, 3, n)

	require.ElementsMatch(t, []replyNotice{
		{reply: first.UUID, parent: parent.UUID},
		{reply: second.UUID, parent: parent.UUID},
	}, notifier.replies)
	require.Empty(t, notifier.moderated)
}
