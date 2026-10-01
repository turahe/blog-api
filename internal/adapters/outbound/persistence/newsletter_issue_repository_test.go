package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func newIssue(lists []string, mutate func(*domain.Issue)) domain.Issue {
	now := time.Now().UTC().Truncate(time.Microsecond)

	issue := domain.Issue{
		UUID: uuid.New(), Subject: "Weekly", BodyMarkdown: "Hello", Lists: lists, Status: domain.IssueDraft,
		CreatedAt: now, UpdatedAt: now,
	}
	if mutate != nil {
		mutate(&issue)
	}

	return issue
}

func createIssue(t *testing.T, repo *NewsletterRepository, lists []string, mutate func(*domain.Issue)) domain.Issue {
	t.Helper()

	issue := newIssue(lists, mutate)
	require.NoError(t, repo.CreateIssue(t.Context(), issue))

	return issue
}

func issueIDs(issues []domain.Issue) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.UUID)
	}

	return out
}

func recipientsBySubscriber(recipients []domain.Recipient) map[uuid.UUID]domain.Recipient {
	out := make(map[uuid.UUID]domain.Recipient, len(recipients))
	for _, r := range recipients {
		out[r.SubscriberID] = r
	}

	return out
}

func TestNewsletterRepositoryCreateIssue(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	second, first := insertNewsletterList(t, tx, 2, nil), insertNewsletterList(t, tx, 1, nil)
	author := insertUser(t, tx)
	sendAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	issue := createIssue(t, repo, []string{second, first}, func(i *domain.Issue) {
		i.Preheader, i.Status, i.SendAt, i.CreatedBy, i.UpdatedBy = "Preview", domain.IssueScheduled, &sendAt, &author, &author
	})

	got, err := repo.Issue(ctx, issue.UUID)
	require.NoError(t, err)
	require.Equal(t, []string{first, second}, got.Lists, "lists follow list position")
	require.Equal(t, "Weekly", got.Subject)
	require.Equal(t, "Preview", got.Preheader)
	require.Equal(t, "Hello", got.BodyMarkdown)
	require.Equal(t, domain.IssueScheduled, got.Status)
	require.WithinDuration(t, sendAt, *got.SendAt, 0)
	require.Equal(t, &author, got.CreatedBy)
	require.Equal(t, &author, got.UpdatedBy)
	require.Nil(t, got.QueuedAt)
	require.WithinDuration(t, issue.CreatedAt, got.CreatedAt, 0)

	_, err = repo.Issue(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.Issue(canceledContext(t), issue.UUID)
	require.ErrorIs(t, err, context.Canceled)

	err = repo.CreateIssue(ctx, newIssue([]string{first}, func(i *domain.Issue) { i.Subject = "" }))
	require.ErrorContains(t, err, "create newsletter issue")

	_, err = repo.Issue(ctx, issue.UUID)
	require.NoError(t, err, "a failed create leaves the enclosing transaction usable")
}

func TestNewsletterRepositoryUpdateIssue(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	before, after := insertNewsletterList(t, tx, 0, nil), insertNewsletterList(t, tx, 1, nil)
	editor := insertUser(t, tx)
	issue := createIssue(t, repo, []string{before}, nil)
	sendAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	issue.Subject, issue.Lists, issue.UpdatedBy, issue.Status, issue.SendAt = "Updated", []string{after}, &editor, domain.IssueScheduled, &sendAt
	saved, err := repo.UpdateIssue(ctx, issue, domain.IssueDraft)
	require.NoError(t, err)
	require.True(t, saved)

	got, err := repo.Issue(ctx, issue.UUID)
	require.NoError(t, err)
	require.Equal(t, "Updated", got.Subject)
	require.Equal(t, []string{after}, got.Lists)
	require.Equal(t, &editor, got.UpdatedBy)
	require.Equal(t, domain.IssueScheduled, got.Status)

	issue.Subject = "Stale"
	saved, err = repo.UpdateIssue(ctx, issue, domain.IssueDraft)
	require.NoError(t, err)
	require.False(t, saved, "the stored status no longer matches")

	got, err = repo.Issue(ctx, issue.UUID)
	require.NoError(t, err)
	require.Equal(t, "Updated", got.Subject)

	issue.Subject = ""
	_, err = repo.UpdateIssue(ctx, issue, domain.IssueScheduled)
	require.ErrorContains(t, err, "update newsletter issue")

	_, err = repo.Issue(ctx, issue.UUID)
	require.NoError(t, err, "a failed update leaves the enclosing transaction usable")
}

func TestNewsletterRepositoryListIssues(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	list := insertNewsletterList(t, tx, 0, nil)

	// A century ahead, so these are the newest issues in the table.
	base := time.Now().UTC().AddDate(100, 0, 0).Truncate(time.Microsecond)
	at := func(minute int, status domain.IssueStatus) func(*domain.Issue) {
		return func(i *domain.Issue) { i.CreatedAt, i.Status = base.Add(time.Duration(minute)*time.Minute), status }
	}
	oldest := createIssue(t, repo, []string{list}, at(0, domain.IssueDraft))
	middle := createIssue(t, repo, []string{list}, at(1, domain.IssueDraft))
	newest := createIssue(t, repo, []string{list}, at(2, domain.IssueCancelled))

	tests := []struct {
		name      string
		filter    domain.IssueFilter
		want      []uuid.UUID
		wantTotal int64
	}{
		{name: "newest first", filter: domain.IssueFilter{PageRequest: pagination.PageRequest{Page: 1, Limit: 2}}, want: []uuid.UUID{newest.UUID, middle.UUID}, wantTotal: 3},
		{name: "second page", filter: domain.IssueFilter{PageRequest: pagination.PageRequest{Page: 2, Limit: 2}}, want: []uuid.UUID{oldest.UUID}, wantTotal: 3},
		{name: "status", filter: domain.IssueFilter{Status: domain.IssueCancelled, PageRequest: pagination.PageRequest{Page: 1, Limit: 1}}, want: []uuid.UUID{newest.UUID}, wantTotal: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := repo.ListIssues(t.Context(), tt.filter)
			require.NoError(t, err)
			require.NotNil(t, page.Total)
			require.GreaterOrEqual(t, *page.Total, tt.wantTotal)
			require.Equal(t, tt.want, issueIDs(page.Items)[:len(tt.want)])
			require.Equal(t, tt.filter.PageRequest.Page, page.OffsetPage)
			require.Equal(t, tt.filter.PageRequest.Limit, page.OffsetPerPage)
		})
	}

	_, err := repo.ListIssues(canceledContext(t), domain.IssueFilter{PageRequest: pagination.PageRequest{Page: 1, Limit: 1}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewsletterRepositoryDueIssues(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	list := insertNewsletterList(t, tx, 0, nil)

	// A century back, so no other scheduled issue is due by the cutoff.
	due := time.Now().UTC().AddDate(-100, 0, 0).Truncate(time.Microsecond)
	scheduled := func(sendAt time.Time, status domain.IssueStatus) domain.Issue {
		return createIssue(t, repo, []string{list}, func(i *domain.Issue) { i.SendAt, i.Status = &sendAt, status })
	}
	later := scheduled(due, domain.IssueScheduled)
	earlier := scheduled(due.Add(-time.Minute), domain.IssueScheduled)
	scheduled(due.Add(-time.Minute), domain.IssueDraft)
	scheduled(due.Add(2*time.Hour), domain.IssueScheduled)

	got, err := repo.DueIssues(t.Context(), due.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{earlier.UUID, later.UUID}, issueIDs(got))

	got, err = repo.DueIssues(t.Context(), due.Add(time.Hour), 1)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{earlier.UUID}, issueIDs(got))

	got, err = repo.DueIssues(t.Context(), due.Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestNewsletterRepositoryMoveIssue(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list := insertNewsletterList(t, tx, 0, nil)
	issue := createIssue(t, repo, []string{list}, nil)
	start := time.Now().UTC().Truncate(time.Microsecond)
	at := func(minute int) time.Time { return start.Add(time.Duration(minute) * time.Minute) }

	move := func(from []domain.IssueStatus, to domain.IssueStatus, when time.Time) bool {
		t.Helper()

		moved, err := repo.MoveIssue(ctx, issue.UUID, from, to, when)
		require.NoError(t, err)

		return moved
	}
	load := func() domain.Issue {
		t.Helper()

		got, err := repo.Issue(ctx, issue.UUID)
		require.NoError(t, err)

		return got
	}

	require.True(t, move([]domain.IssueStatus{domain.IssueDraft, domain.IssueScheduled}, domain.IssueQueued, at(1)))
	got := load()
	require.Equal(t, domain.IssueQueued, got.Status)
	require.WithinDuration(t, at(1), *got.QueuedAt, 0)
	require.Nil(t, got.StartedAt)
	require.WithinDuration(t, at(1), got.UpdatedAt, 0)

	require.False(t, move([]domain.IssueStatus{domain.IssueDraft}, domain.IssueSending, at(2)), "not in a from status")

	require.True(t, move([]domain.IssueStatus{domain.IssueQueued}, domain.IssueSending, at(3)))
	got = load()
	require.WithinDuration(t, at(3), *got.StartedAt, 0)
	require.WithinDuration(t, at(1), *got.QueuedAt, 0, "queued_at is only stamped when queueing")

	require.True(t, move([]domain.IssueStatus{domain.IssueSending}, domain.IssueQueued, at(4)))
	require.True(t, move([]domain.IssueStatus{domain.IssueQueued}, domain.IssueSending, at(5)))
	got = load()
	require.WithinDuration(t, at(4), *got.QueuedAt, 0)
	require.WithinDuration(t, at(3), *got.StartedAt, 0, "started_at keeps the first start")

	_, err := repo.MoveIssue(canceledContext(t), issue.UUID, []domain.IssueStatus{domain.IssueSending}, domain.IssueSent, at(6))
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewsletterRepositoryDeliveryLifecycle(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list, other := insertNewsletterList(t, tx, 0, nil), insertNewsletterList(t, tx, 1, nil)
	issue := createIssue(t, repo, []string{list}, func(i *domain.Issue) { i.Status = domain.IssueSending })

	subscribe := func(status domain.Status, memberships ...domain.Membership) func(*domain.Subscriber) {
		return func(s *domain.Subscriber) { s.Status, s.Memberships = status, memberships }
	}
	named := createSubscriber(t, repo, func(s *domain.Subscriber) {
		s.DisplayName, s.Format, s.Memberships = "Ann", domain.FormatPlaintext, []domain.Membership{member(list, domain.MembershipActive)}
	})
	plain := createSubscriber(t, repo, subscribe(domain.StatusActive, member(list, domain.MembershipActive)))
	createSubscriber(t, repo, subscribe(domain.StatusActive, member(list, domain.MembershipLeft)))
	createSubscriber(t, repo, subscribe(domain.StatusUnsubscribed, member(list, domain.MembershipActive)))
	createSubscriber(t, repo, subscribe(domain.StatusActive, member(other, domain.MembershipActive)))

	now := time.Now().UTC().Truncate(time.Microsecond)
	claim := domain.Claim{Limit: 10, MaxAttempts: 2, Now: now, StaleBefore: now.Add(-time.Hour), RetryBefore: now.Add(-time.Minute)}

	recipients, err := repo.ClaimRecipients(ctx, issue.UUID, claim)
	require.NoError(t, err)
	require.Len(t, recipients, 2, "only active subscribers with an active membership of the issue's lists")

	byID := recipientsBySubscriber(recipients)
	require.Equal(t, named.Email, byID[named.UUID].Email)
	require.Equal(t, "Ann", byID[named.UUID].Name)
	require.Equal(t, domain.FormatPlaintext, byID[named.UUID].Format)
	require.Equal(t, 1, byID[named.UUID].Attempts)
	require.Empty(t, byID[plain.UUID].Name)
	require.NotEqual(t, uuid.Nil, byID[plain.UUID].DeliveryID)

	again, err := repo.ClaimRecipients(ctx, issue.UUID, claim)
	require.NoError(t, err)
	require.Empty(t, again, "fresh claims are not reclaimed")

	require.NoError(t, repo.RecordDelivery(ctx, domain.DeliveryResult{DeliveryID: byID[named.UUID].DeliveryID, Sent: true, At: now}))
	require.NoError(t, repo.RecordDelivery(ctx, domain.DeliveryResult{DeliveryID: byID[plain.UUID].DeliveryID, Error: "timeout", At: now}))

	counts, err := repo.DeliveryCounts(ctx, issue.UUID, claim.MaxAttempts)
	require.NoError(t, err)
	require.Equal(t, domain.DeliveryCounts{Sent: 1, Retryable: 1}, counts)

	claim.RetryBefore = now.Add(time.Second)
	retried, err := repo.ClaimRecipients(ctx, issue.UUID, claim)
	require.NoError(t, err)
	require.Len(t, retried, 1, "failures older than RetryBefore are retried")
	require.Equal(t, plain.UUID, retried[0].SubscriberID)
	require.Equal(t, 2, retried[0].Attempts)

	require.NoError(t, repo.RecordDelivery(ctx, domain.DeliveryResult{DeliveryID: retried[0].DeliveryID, Error: "timeout", At: now}))

	counts, err = repo.DeliveryCounts(ctx, issue.UUID, claim.MaxAttempts)
	require.NoError(t, err)
	require.Equal(t, domain.DeliveryCounts{Sent: 1, Failed: 1}, counts, "out of attempts")

	exhausted, err := repo.ClaimRecipients(ctx, issue.UUID, claim)
	require.NoError(t, err)
	require.Empty(t, exhausted)

	finished, err := repo.FinishIssue(ctx, issue.UUID, counts, now)
	require.NoError(t, err)
	require.True(t, finished)

	finished, err = repo.FinishIssue(ctx, issue.UUID, counts, now)
	require.NoError(t, err)
	require.False(t, finished, "only a sending issue can finish")

	got, err := repo.Issue(ctx, issue.UUID)
	require.NoError(t, err)
	require.Equal(t, domain.IssueSent, got.Status)
	require.Equal(t, 1, got.SentCount)
	require.Equal(t, 1, got.FailedCount)
	require.WithinDuration(t, now, *got.CompletedAt, 0)
}

func TestNewsletterRepositoryDeliveryCountsUnreachable(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := t.Context()
	list := insertNewsletterList(t, tx, 0, nil)
	issue := createIssue(t, repo, []string{list}, func(i *domain.Issue) { i.Status = domain.IssueSending })
	sub := createSubscriber(t, repo, func(s *domain.Subscriber) {
		s.Memberships = []domain.Membership{member(list, domain.MembershipActive)}
	})
	now := time.Now().UTC()

	recipients, err := repo.ClaimRecipients(ctx, issue.UUID, domain.Claim{
		Limit: 10, MaxAttempts: 5, Now: now, StaleBefore: now.Add(-time.Hour), RetryBefore: now,
	})
	require.NoError(t, err)
	require.Len(t, recipients, 1)
	require.NoError(t, repo.RecordDelivery(ctx, domain.DeliveryResult{DeliveryID: recipients[0].DeliveryID, Error: "timeout", At: now}))

	loaded, err := repo.Subscriber(ctx, sub.UUID)
	require.NoError(t, err)
	loaded.SetMembership(list, domain.MembershipLeft, now)
	require.NoError(t, repo.UpdateSubscriber(ctx, loaded))

	counts, err := repo.DeliveryCounts(ctx, issue.UUID, 5)
	require.NoError(t, err)
	require.Equal(t, domain.DeliveryCounts{Failed: 1}, counts, "a failure to someone who left is final")
}

func TestNewsletterRepositoryIssueDatabaseErrors(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewNewsletterRepository(tx)
	ctx := canceledContext(t)
	id := uuid.New()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "create issue", call: func() error { return repo.CreateIssue(ctx, newIssue([]string{"x"}, nil)) }},
		{name: "update issue", call: func() error { _, err := repo.UpdateIssue(ctx, newIssue(nil, nil), domain.IssueDraft); return err }},
		{name: "due issues", call: func() error { _, err := repo.DueIssues(ctx, time.Now(), 1); return err }},
		{name: "claim recipients", call: func() error { _, err := repo.ClaimRecipients(ctx, id, domain.Claim{Limit: 1}); return err }},
		{name: "record delivery", call: func() error { return repo.RecordDelivery(ctx, domain.DeliveryResult{DeliveryID: id}) }},
		{name: "delivery counts", call: func() error { _, err := repo.DeliveryCounts(ctx, id, 1); return err }},
		{name: "finish issue", call: func() error { _, err := repo.FinishIssue(ctx, id, domain.DeliveryCounts{}, time.Now()); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.call(), context.Canceled)
		})
	}
}
