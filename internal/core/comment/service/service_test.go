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
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type seqIDs struct{}

func (seqIDs) New() uuid.UUID { return uuid.New() }

type fakeRepo struct {
	publicPosts map[uuid.UUID]bool
	policies    map[uuid.UUID]commentdomain.Policy
	comments    map[uuid.UUID]commentdomain.Comment
	flags       []commentdomain.Flag
	upvotes     map[uuid.UUID]map[uuid.UUID]bool
	log         []commentdomain.ModerationEntry
	lastFilter  commentdomain.ListFilter

	policyErr     error
	getErr        error
	getByIDsErr   error
	listErr       error
	createErr     error
	applyErr      error
	hardDeleteErr error
	listFlagsErr  error
	logErr        error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		publicPosts: map[uuid.UUID]bool{},
		policies:    map[uuid.UUID]commentdomain.Policy{},
		comments:    map[uuid.UUID]commentdomain.Comment{},
		upvotes:     map[uuid.UUID]map[uuid.UUID]bool{},
	}
}

func (r *fakeRepo) PostPolicy(_ context.Context, postID uuid.UUID) (commentdomain.Policy, error) {
	if r.policyErr != nil {
		return "", r.policyErr
	}

	if !r.publicPosts[postID] {
		return "", commentdomain.ErrPostNotFound
	}

	if policy, ok := r.policies[postID]; ok {
		return policy, nil
	}

	return commentdomain.PolicyOpen, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (commentdomain.Comment, error) {
	if r.getErr != nil {
		return commentdomain.Comment{}, r.getErr
	}

	c, ok := r.comments[id]
	if !ok {
		return commentdomain.Comment{}, commentdomain.ErrNotFound
	}

	return c, nil
}

func (r *fakeRepo) List(_ context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	r.lastFilter = filter
	if r.listErr != nil {
		return commentdomain.ListResult{}, r.listErr
	}

	var totalPtr *int64
	if filter.IncludeTotal {
		var t int64 = 0
		totalPtr = &t
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	return commentdomain.ListResult{OffsetPage: page, OffsetPerPage: limit, Total: totalPtr, Limit: limit}, nil
}

func (r *fakeRepo) ListPublic(_ context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	r.lastFilter = filter
	if r.listErr != nil {
		return commentdomain.ListResult{}, r.listErr
	}

	var totalPtr *int64
	if filter.IncludeTotal {
		var t int64 = 0
		totalPtr = &t
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	return commentdomain.ListResult{OffsetPage: page, OffsetPerPage: limit, Total: totalPtr, Limit: limit}, nil
}

func (r *fakeRepo) ListForMe(_ context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	r.lastFilter = filter
	if r.listErr != nil {
		return commentdomain.ListResult{}, r.listErr
	}

	var totalPtr *int64
	if filter.IncludeTotal {
		var t int64 = 0
		totalPtr = &t
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	return commentdomain.ListResult{OffsetPage: page, OffsetPerPage: limit, Total: totalPtr, Limit: limit}, nil
}

func (r *fakeRepo) ListAdmin(_ context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	r.lastFilter = filter
	if r.listErr != nil {
		return commentdomain.ListResult{}, r.listErr
	}

	var totalPtr *int64
	if filter.IncludeTotal {
		var t int64 = 0
		totalPtr = &t
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	return commentdomain.ListResult{OffsetPage: page, OffsetPerPage: limit, Total: totalPtr, Limit: limit}, nil
}

func (r *fakeRepo) Create(_ context.Context, c commentdomain.Comment) (commentdomain.Comment, error) {
	if r.createErr != nil {
		return commentdomain.Comment{}, r.createErr
	}

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
	if r.getByIDsErr != nil {
		return nil, r.getByIDsErr
	}

	var found []commentdomain.Comment

	for _, id := range ids {
		if c, ok := r.comments[id]; ok {
			found = append(found, c)
		}
	}

	return found, nil
}

func (r *fakeRepo) ApplyModerations(_ context.Context, changes []commentdomain.Moderation) error {
	if r.applyErr != nil {
		return r.applyErr
	}

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
	if r.hardDeleteErr != nil {
		return false, r.hardDeleteErr
	}

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
	if r.listFlagsErr != nil {
		return nil, r.listFlagsErr
	}

	var flags []commentdomain.Flag

	for _, flag := range r.flags {
		if flag.CommentUUID == id {
			flags = append(flags, flag)
		}
	}

	return flags, nil
}

func (r *fakeRepo) ListModerationLog(_ context.Context, id uuid.UUID) ([]commentdomain.ModerationEntry, error) {
	if r.logErr != nil {
		return nil, r.logErr
	}

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

type prefixHasher struct{}

func (prefixHasher) MAC(value string) string { return "mac:" + value }

func TestIdentityHashesAreKeyedWhenAHasherIsSet(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{IdentityHasher: prefixHasher{}})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorUUID: &f.user, Content: "hello", ClientIP: "203.0.113.9",
	})
	require.NoError(t, err)
	require.Equal(t, "mac:203.0.113.9", got.IPHash)

	require.NoError(t, f.svc.Flag(context.Background(), FlagInput{
		CommentUUID: got.UUID, Reason: "spam", ClientIP: "198.51.100.1", UserAgent: "curl",
	}))
	require.Len(t, f.repo.flags, 1)
	require.Equal(t, "mac:198.51.100.1|curl", f.repo.flags[0].ReporterIPHash)
}

func TestCreateTruncatesTheUserAgent(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	got, err := f.svc.Create(context.Background(), CreateInput{
		PostUUID: f.post, AuthorUUID: &f.user, Content: "hi", UserAgent: " " + strings.Repeat("é", maxUserAgentRunes+10) + " ",
	})
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", maxUserAgentRunes), got.UserAgent)
}

func TestCreateFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	parentID := uuid.New()

	tests := []struct {
		name    string
		cfg     Config
		breakIt func(*fakeRepo)
		in      func(f fixture) CreateInput
	}{
		{
			name:    "store fails",
			breakIt: func(r *fakeRepo) { r.createErr = boom },
			in:      func(f fixture) CreateInput { return CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"} },
		},
		{
			name:    "parent lookup fails",
			breakIt: func(r *fakeRepo) { r.getErr = boom },
			in: func(f fixture) CreateInput {
				return CreateInput{PostUUID: f.post, ParentUUID: &parentID, AuthorUUID: &f.user, Content: "hi"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(tt.cfg)
			tt.breakIt(f.repo)

			_, err := f.svc.Create(t.Context(), tt.in(f))
			require.ErrorIs(t, err, boom)
		})
	}
}

func TestCreateGuestNameTooLong(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{GuestEnabled: true})

	_, err := f.svc.Create(t.Context(), CreateInput{
		PostUUID: f.post, AuthorName: strings.Repeat("n", maxAuthorNameRunes+1), AuthorEmail: "ann@example.com", Content: "hi",
	})
	require.ErrorIs(t, err, commentdomain.ErrValidation)
	require.Empty(t, f.repo.comments)
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

func TestUpdateFailures(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{AuthorUUID: &f.user, Content: "old"})

	_, err := f.svc.Update(t.Context(), f.user, c.UUID, "   ")
	require.ErrorIs(t, err, commentdomain.ErrValidation)

	_, err = f.svc.Update(t.Context(), f.user, uuid.New(), "new")
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
	require.Equal(t, "old", f.repo.comments[c.UUID].Content)
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

func TestDeleteMissingComment(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	require.ErrorIs(t, f.svc.Delete(t.Context(), f.user, uuid.New()), commentdomain.ErrNotFound)
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

func TestFlagRejections(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})
	c := f.seed(commentdomain.Comment{})

	err := f.svc.Flag(t.Context(), FlagInput{
		CommentUUID: c.UUID, Reason: "spam", Details: strings.Repeat("d", commentdomain.MaxFlagDetailsRunes+1),
	})
	require.ErrorIs(t, err, commentdomain.ErrValidation)

	err = f.svc.Flag(t.Context(), FlagInput{CommentUUID: uuid.New(), Reason: "spam"})
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
	require.Empty(t, f.repo.flags)
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

func TestToggleUpvoteMissingComment(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	_, _, err := f.svc.ToggleUpvote(t.Context(), f.user, uuid.New())
	require.ErrorIs(t, err, commentdomain.ErrNotFound)
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

	filter := commentdomain.ListFilter{PostUUID: &f.post}
	_, err := f.svc.ListForPost(context.Background(), filter)
	require.NoError(t, err)
	require.True(t, f.repo.lastFilter.RootsOnly)
	require.Equal(t, 1, f.repo.lastFilter.Page)
	require.Equal(t, 20, f.repo.lastFilter.Limit)

	postID := uuid.New()
	filter2 := commentdomain.ListFilter{PostUUID: &postID}
	_, err = f.svc.ListForPost(context.Background(), filter2)
	require.ErrorIs(t, err, commentdomain.ErrPostNotFound)
}

func TestGetThreadFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		breakIt func(*fakeRepo)
		missing bool
		wantErr error
	}{
		{name: "missing comment", breakIt: func(*fakeRepo) {}, missing: true, wantErr: commentdomain.ErrNotFound},
		{name: "post policy fails", breakIt: func(r *fakeRepo) { r.policyErr = boom }, wantErr: boom},
		{name: "replies fail", breakIt: func(r *fakeRepo) { r.listErr = boom }, wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(Config{})
			id := f.seed(commentdomain.Comment{Content: "hi"}).UUID
			if tt.missing {
				id = uuid.New()
			}

			tt.breakIt(f.repo)

			_, err := f.svc.GetThread(t.Context(), id)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestListMineShowsEveryLiveStatusNewestFirst(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{})

	filter := commentdomain.ListFilter{AuthorUUID: &f.user}
	got, err := f.svc.ListMine(t.Context(), filter)
	require.NoError(t, err)
	require.Equal(t, 1, got.OffsetPage)
	require.Equal(t, 20, got.OffsetPerPage)
	require.Equal(t, &f.user, f.repo.lastFilter.AuthorUUID)
	require.True(t, f.repo.lastFilter.NewestFirst)
	require.ElementsMatch(t, []commentdomain.Status{
		commentdomain.StatusPending, commentdomain.StatusApproved, commentdomain.StatusFlagged,
		commentdomain.StatusSpam, commentdomain.StatusRejected,
	}, f.repo.lastFilter.Statuses)
	require.NotContains(t, f.repo.lastFilter.Statuses, commentdomain.StatusDeleted)
}
