package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type seqIDs struct{}

func (seqIDs) New() uuid.UUID { return uuid.New() }

type fakeRepo struct {
	publicPosts map[uuid.UUID]bool
	comments    map[uuid.UUID]commentdomain.Comment
	flags       []commentdomain.Flag
	upvotes     map[uuid.UUID]map[uuid.UUID]bool
	log         []commentdomain.ModerationEntry
	lastFilter  commentdomain.ListFilter
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		publicPosts: map[uuid.UUID]bool{},
		comments:    map[uuid.UUID]commentdomain.Comment{},
		upvotes:     map[uuid.UUID]map[uuid.UUID]bool{},
	}
}

func (r *fakeRepo) PostIsPublic(_ context.Context, postID uuid.UUID) error {
	if !r.publicPosts[postID] {
		return commentdomain.ErrPostNotFound
	}

	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (commentdomain.Comment, error) {
	c, ok := r.comments[id]
	if !ok {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	return c, nil
}

func (r *fakeRepo) List(_ context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	r.lastFilter = filter
	return commentdomain.ListResult{Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *fakeRepo) Create(_ context.Context, c commentdomain.Comment) (commentdomain.Comment, error) {
	r.comments[c.UUID] = c
	return c, nil
}

func (r *fakeRepo) Update(_ context.Context, c commentdomain.Comment) (commentdomain.Comment, error) {
	r.comments[c.UUID] = c
	return c, nil
}

func (r *fakeRepo) AddFlag(_ context.Context, flag commentdomain.Flag, _ int) (bool, error) {
	r.flags = append(r.flags, flag)
	return true, nil
}

func (r *fakeRepo) ToggleUpvote(_ context.Context, commentID, voterID uuid.UUID, _ time.Time) (bool, int, error) {
	if r.upvotes[commentID] == nil {
		r.upvotes[commentID] = map[uuid.UUID]bool{}
	}

	r.upvotes[commentID][voterID] = !r.upvotes[commentID][voterID]
	count := 0

	for _, on := range r.upvotes[commentID] {
		if on {
			count++
		}
	}

	return r.upvotes[commentID][voterID], count, nil
}

func (r *fakeRepo) GetByIDs(_ context.Context, ids []uuid.UUID) ([]commentdomain.Comment, error) {
	var found []commentdomain.Comment

	for _, id := range ids {
		if c, ok := r.comments[id]; ok {
			found = append(found, c)
		}
	}

	return found, nil
}

func (r *fakeRepo) ApplyModerations(_ context.Context, changes []commentdomain.Moderation) error {
	var stale []uuid.UUID

	for _, change := range changes {
		if r.comments[change.Comment.UUID].Status != change.From {
			stale = append(stale, change.Comment.UUID)
		}
	}

	if len(stale) > 0 {
		return &commentdomain.BatchError{Err: commentdomain.ErrInvalidTransition, IDs: stale}
	}

	for _, change := range changes {
		r.comments[change.Comment.UUID] = change.Comment
		r.log = append(r.log, change.Entry)
	}

	return nil
}

func (r *fakeRepo) HardDelete(_ context.Context, id uuid.UUID, entry commentdomain.ModerationEntry) (bool, error) {
	c, ok := r.comments[id]
	if !ok {
		return false, commentdomain.ErrNotFound
	}

	r.log = append(r.log, entry)

	for _, other := range r.comments {
		if other.ParentUUID != nil && *other.ParentUUID == id {
			c.Content, c.AuthorUUID, c.AuthorName, c.AuthorEmail = "", nil, "", ""
			c.Status = commentdomain.StatusDeleted
			r.comments[id] = c

			return true, nil
		}
	}

	delete(r.comments, id)

	return false, nil
}

func (r *fakeRepo) ListFlags(_ context.Context, id uuid.UUID) ([]commentdomain.Flag, error) {
	var flags []commentdomain.Flag

	for _, flag := range r.flags {
		if flag.CommentUUID == id {
			flags = append(flags, flag)
		}
	}

	return flags, nil
}

func (r *fakeRepo) ListModerationLog(_ context.Context, id uuid.UUID) ([]commentdomain.ModerationEntry, error) {
	var entries []commentdomain.ModerationEntry

	for _, entry := range r.log {
		if entry.CommentUUID == id {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

func (r *fakeRepo) Stats(context.Context) (commentdomain.Stats, error) {
	return commentdomain.Stats{QueueDepth: int64(len(r.comments))}, nil
}

type fixture struct {
	svc   *Service
	repo  *fakeRepo
	clock *fixedClock
	post  uuid.UUID
	user  uuid.UUID
}

func newFixture(cfg Config) fixture {
	repo := newFakeRepo()
	post := uuid.New()
	repo.publicPosts[post] = true
	clock := &fixedClock{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}

	return fixture{svc: New(repo, seqIDs{}, clock, cfg), repo: repo, clock: clock, post: post, user: uuid.New()}
}

func (f fixture) seed(c commentdomain.Comment) commentdomain.Comment {
	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}

	if c.PostUUID == uuid.Nil {
		c.PostUUID = f.post
	}

	if c.Status == "" {
		c.Status = commentdomain.StatusApproved
	}

	if c.CreatedAt.IsZero() {
		c.CreatedAt = f.clock.now
	}

	f.repo.comments[c.UUID] = c

	return c
}

func TestCreateByUserIsApprovedAndHashesIP(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorUUID: &f.user, Content: "  hello  ", ClientIP: "203.0.113.9",
	})

	require.NoError(t, err)
	require.Equal(t, "hello", got.Content)
	require.Equal(t, commentdomain.StatusApproved, got.Status)
	require.Equal(t, 0, got.Depth)
	require.Len(t, got.IPHash, 64)
	require.NotContains(t, got.IPHash, "203.0.113.9")
}

func TestCreateRequireApprovalStartsPending(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{RequireApproval: true})

	got, err := f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"})

	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusPending, got.Status)
}

func TestCreateGuestDisabled(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorName: "Ann", AuthorEmail: "ann@example.com", Content: "hi",
	})

	require.ErrorIs(t, err, commentdomain.ErrGuestDisabled)
}

func TestCreateGuestRequiresIdentityAndStartsPending(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{GuestEnabled: true})

	_, err := f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorName: "Ann", Content: "hi"})
	require.ErrorIs(t, err, commentdomain.ErrValidation)

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorName: "Ann", AuthorEmail: "Ann@Example.com", Content: "hi",
	})
	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusPending, got.Status)
	require.Equal(t, "ann@example.com", got.AuthorEmail)
}

func TestCreateHoneypotMarksSpam(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorUUID: &f.user, Content: "buy now", Honeypot: "http://spam",
	})

	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusSpam, got.Status)
}

func TestCreateRejectsUnpublishedPost(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, err := f.svc.Create(context.Background(), CreateInput{PostUUID: uuid.New(), AuthorUUID: &f.user, Content: "hi"})

	require.ErrorIs(t, err, commentdomain.ErrPostNotFound)
}

func TestCreateContentValidation(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, err := f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "   "})
	require.ErrorIs(t, err, commentdomain.ErrValidation)

	long := make([]rune, commentdomain.MaxContentRunes+1)
	for i := range long {
		long[i] = 'é'
	}

	_, err = f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: string(long)})
	require.ErrorIs(t, err, commentdomain.ErrValidation)
}

func TestCreateReplySetsDepth(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	parent := f.seed(commentdomain.Comment{Depth: 2})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, ParentUUID: &parent.UUID, AuthorUUID: &f.user, Content: "reply",
	})

	require.NoError(t, err)
	require.Equal(t, 3, got.Depth)
	require.Equal(t, parent.UUID, *got.ParentUUID)
}

func TestCreateReplyDepthLimit(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	parent := f.seed(commentdomain.Comment{Depth: commentdomain.MaxDepth})

	_, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, ParentUUID: &parent.UUID, AuthorUUID: &f.user, Content: "too deep",
	})

	require.ErrorIs(t, err, commentdomain.ErrDepthExceeded)
}

func TestCreateReplyRejectsInvalidParent(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	otherPost := uuid.New()
	f.repo.publicPosts[otherPost] = true
	cases := map[string]commentdomain.Comment{
		"other post": f.seed(commentdomain.Comment{PostUUID: otherPost}),
		"pending":    f.seed(commentdomain.Comment{Status: commentdomain.StatusPending}),
		"deleted":    f.seed(commentdomain.Comment{Status: commentdomain.StatusDeleted}),
	}
	missing := uuid.New()

	for name, parent := range cases {
		_, err := f.svc.Create(context.Background(), CreateInput{
			PostUUID: f.post, ParentUUID: &parent.UUID, AuthorUUID: &f.user, Content: "x",
		})
		require.ErrorIs(t, err, commentdomain.ErrParentInvalid, name)
	}

	_, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, ParentUUID: &missing, AuthorUUID: &f.user, Content: "x",
	})
	require.ErrorIs(t, err, commentdomain.ErrParentInvalid)
}

func TestUpdateWithinEditWindow(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{EditWindow: 15 * time.Minute})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Content: "old"})
	f.clock.now = f.clock.now.Add(14 * time.Minute)

	got, err := f.svc.Update(context.Background(), f.user, c.UUID, "new")

	require.NoError(t, err)
	require.Equal(t, "new", got.Content)
	require.NotNil(t, got.EditedAt)
}

func TestUpdateAfterEditWindowExpires(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{EditWindow: 15 * time.Minute})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Content: "old"})
	f.clock.now = f.clock.now.Add(15*time.Minute + time.Second)

	_, err := f.svc.Update(context.Background(), f.user, c.UUID, "new")

	require.ErrorIs(t, err, commentdomain.ErrEditWindowClosed)
}

func TestUpdateRejectsOtherUsersAndGuestComments(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	owned := f.seed(commentdomain.Comment{AuthorUUID: &f.user})
	guest := f.seed(commentdomain.Comment{AuthorName: "Ann"})
	intruder := uuid.New()

	_, err := f.svc.Update(context.Background(), intruder, owned.UUID, "hijack")
	require.ErrorIs(t, err, commentdomain.ErrForbidden)
	_, err = f.svc.Update(context.Background(), intruder, guest.UUID, "hijack")
	require.ErrorIs(t, err, commentdomain.ErrForbidden)
	require.Empty(t, f.repo.comments[owned.UUID].Content)
}

func TestUpdateRejectsSpamAndDeleted(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	spam := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Status: commentdomain.StatusSpam})
	deleted := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Status: commentdomain.StatusDeleted})

	_, err := f.svc.Update(context.Background(), f.user, spam.UUID, "x")
	require.ErrorIs(t, err, commentdomain.ErrNotEditable)
	_, err = f.svc.Update(context.Background(), f.user, deleted.UUID, "x")
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}

func TestDeleteSoftDeletesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user})

	require.NoError(t, f.svc.Delete(context.Background(), f.user, c.UUID))
	stored := f.repo.comments[c.UUID]
	require.Equal(t, commentdomain.StatusDeleted, stored.Status)
	require.NotNil(t, stored.DeletedAt)
	require.Equal(t, f.user, *stored.DeletedByUUID)

	require.NoError(t, f.svc.Delete(context.Background(), f.user, c.UUID))
}

func TestDeleteRejectsNonOwner(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user})

	err := f.svc.Delete(context.Background(), uuid.New(), c.UUID)

	require.ErrorIs(t, err, commentdomain.ErrForbidden)
	require.Equal(t, commentdomain.StatusApproved, f.repo.comments[c.UUID].Status)
}

func TestFlagValidatesReasonAndHashesGuestIdentity(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{})

	err := f.svc.Flag(context.Background(), FlagInput{CommentUUID: c.UUID, Reason: "boring"})
	require.ErrorIs(t, err, commentdomain.ErrValidation)

	err = f.svc.Flag(context.Background(), FlagInput{
		CommentUUID: c.UUID, Reason: "spam", ClientIP: "198.51.100.1", UserAgent: "curl",
	})
	require.NoError(t, err)
	require.Len(t, f.repo.flags, 1)
	require.Nil(t, f.repo.flags[0].ReporterUUID)
	require.Len(t, f.repo.flags[0].ReporterIPHash, 64)
}

func TestFlagAndUpvoteRequireApprovedComment(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	pending := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending})

	err := f.svc.Flag(context.Background(), FlagInput{CommentUUID: pending.UUID, Reason: "spam", ReporterUUID: &f.user})
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
	_, _, err = f.svc.ToggleUpvote(context.Background(), f.user, pending.UUID)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
}

func TestToggleUpvote(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{})

	upvoted, count, err := f.svc.ToggleUpvote(context.Background(), f.user, c.UUID)
	require.NoError(t, err)
	require.True(t, upvoted)
	require.Equal(t, 1, count)

	upvoted, count, err = f.svc.ToggleUpvote(context.Background(), f.user, c.UUID)
	require.NoError(t, err)
	require.False(t, upvoted)
	require.Equal(t, 0, count)
}

func TestGetThreadHidesNonPublicComments(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	pending := f.seed(commentdomain.Comment{Status: commentdomain.StatusPending})
	onHiddenPost := f.seed(commentdomain.Comment{PostUUID: uuid.New()})
	visible := f.seed(commentdomain.Comment{})

	_, err := f.svc.GetThread(context.Background(), pending.UUID)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
	_, err = f.svc.GetThread(context.Background(), onHiddenPost.UUID)
	require.ErrorIs(t, err, commentdomain.ErrNotFound)

	thread, err := f.svc.GetThread(context.Background(), visible.UUID)
	require.NoError(t, err)
	require.Equal(t, visible.UUID, thread.Comment.UUID)
	require.Equal(t, visible.UUID, *f.repo.lastFilter.ParentUUID)
	require.Equal(t, commentdomain.PublicStatuses, f.repo.lastFilter.Statuses)
}

func TestListForPostDefaultsToRoots(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, err := f.svc.ListForPost(context.Background(), f.post, nil, 0, 500)
	require.NoError(t, err)
	require.True(t, f.repo.lastFilter.RootsOnly)
	require.Equal(t, 1, f.repo.lastFilter.Page)
	require.Equal(t, 20, f.repo.lastFilter.PerPage)

	_, err = f.svc.ListForPost(context.Background(), uuid.New(), nil, 1, 20)
	require.ErrorIs(t, err, commentdomain.ErrPostNotFound)
}
