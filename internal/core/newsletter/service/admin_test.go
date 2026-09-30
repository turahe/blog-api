package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var testActor = uuid.MustParse("0b6c3a52-7e1f-4d2a-8c9b-5e4f3a2b1c0d")

func auditEntry(scope *audit.Scope) auditdomain.Entry {
	var entry auditdomain.Entry
	scope.Apply(&entry)

	return entry
}

func TestProviderConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *fixture)
		wantErr error
	}{
		{name: "settings and every list"},
		{name: "config unavailable", setup: func(f *fixture) { f.repo.failOn("Config", errBoom) }, wantErr: errBoom},
		{name: "lists unavailable", setup: func(f *fixture) { f.repo.failOn("Lists(archived)", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			got, err := f.svc.ProviderConfig(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "1 Example Street", got.Config.PostalAddress)
			require.Len(t, got.Lists, 3, "archived lists are included")
		})
	}
}

func TestSaveProviderConfig(t *testing.T) {
	t.Parallel()

	valid := domain.Config{
		FromName: "New name", FromEmail: "news@example.test", ReplyTo: "reply@example.test",
		PostalAddress: "1 Example Street", ConfirmTTL: 24 * time.Hour,
	}
	lists := []domain.ListInput{
		{Slug: "weekly", Name: "Weekly", IsDefault: true},
		{Slug: "events", Name: "Events"},
	}

	tests := []struct {
		name    string
		cfg     domain.Config
		lists   []domain.ListInput
		setup   func(f *fixture)
		wantErr error
	}{
		{name: "replaces settings and lists", cfg: valid, lists: lists},
		{name: "invalid settings", cfg: domain.Config{}, lists: lists, wantErr: domain.ErrValidation},
		{name: "invalid lists", cfg: valid, wantErr: domain.ErrValidation},
		{
			name: "current settings unavailable", cfg: valid, lists: lists,
			setup:   func(f *fixture) { f.repo.failOn("Config", errBoom) },
			wantErr: errBoom,
		},
		{
			name: "settings save fails", cfg: valid, lists: lists,
			setup:   func(f *fixture) { f.repo.failOn("SaveConfig", errBoom) },
			wantErr: errBoom,
		},
		{
			name: "lists save fails", cfg: valid, lists: lists,
			setup:   func(f *fixture) { f.repo.failOn("SaveLists", errBoom) },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			ctx, scope := audit.WithScope(t.Context())

			got, err := f.svc.SaveProviderConfig(ctx, testActor, tt.cfg, tt.lists)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "New name", got.Config.FromName)
			assert.Equal(t, testNow, *got.Config.UpdatedAt)
			assert.Equal(t, testActor, *got.Config.UpdatedBy)

			active := map[string]bool{}
			for _, l := range got.Lists {
				active[l.Slug] = l.ArchivedAt == nil
			}

			assert.Equal(t, map[string]bool{"weekly": true, "events": true, "product": false, "old": false}, active)

			entry := auditEntry(scope)
			assert.Equal(t, map[string]auditdomain.Change{
				"from_name":             {From: "Example", To: "New name"},
				"confirm_ttl_hours":     {From: 48, To: 24},
				"double_optin_required": {From: true, To: false},
			}, entry.Changes)
			assert.Equal(t, []string{"weekly", "events"}, entry.Metadata["lists"])
		})
	}
}

func TestListSubscribers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filter      domain.SubscriberFilter
		setup       func(f *fixture)
		wantStatus  domain.Status
		wantQuery   string
		wantPage    int
		wantPerPage int
		wantErr     error
	}{
		{
			name:        "defaults the page",
			filter:      domain.SubscriberFilter{Query: "reader"},
			wantQuery:   "reader",
			wantPage:    1,
			wantPerPage: defaultPerPage,
		},
		{
			name:        "caps the page size",
			filter:      domain.SubscriberFilter{Status: domain.StatusActive, PageRequest: pagination.PageRequest{Page: 3, Limit: 500}},
			wantStatus:  domain.StatusActive,
			wantPage:    3,
			wantPerPage: maxPerPage,
		},
		{name: "unknown status", filter: domain.SubscriberFilter{Status: "gone"}, wantErr: domain.ErrValidation},
		{name: "store fails", setup: func(f *fixture) { f.repo.failOn("ListSubscribers", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			got, err := f.svc.ListSubscribers(t.Context(), tt.filter)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, f.repo.subscriberFilter.Status)
			require.Equal(t, tt.wantQuery, f.repo.subscriberFilter.Query)
			require.Equal(t, tt.wantPage, f.repo.subscriberFilter.PageRequest.Page)
			require.Equal(t, tt.wantPerPage, f.repo.subscriberFilter.PageRequest.Limit)
			require.Equal(t, tt.wantPerPage, got.OffsetPerPage)
			require.Equal(t, tt.wantPerPage, got.Limit)
		})
	}
}

func TestSubscriberDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *fixture)
		wantErr error
	}{
		{name: "subscriber with history"},
		{name: "subscriber lookup fails", setup: func(f *fixture) { f.repo.failOn("Subscriber", errBoom) }, wantErr: errBoom},
		{name: "history fails", setup: func(f *fixture) { f.repo.failOn("ConsentHistory", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			sub := f.activeSubscriber("reader@example.test", "weekly")
			require.NoError(t, f.repo.AppendConsent(t.Context(), domain.ConsentEvent{SubscriberID: sub.UUID, Event: domain.ConsentConfirmed}))

			if tt.setup != nil {
				tt.setup(f)
			}

			got, err := f.svc.Subscriber(t.Context(), sub.UUID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, sub.UUID, got.Subscriber.UUID)
			require.Len(t, got.History, 1)
			require.Equal(t, historyLimit, f.repo.historyLimit)
		})
	}
}

func TestDeleteSubscriber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        string
		setup       func(f *fixture, sub domain.Subscriber)
		wantErr     error
		wantStatus  domain.Status
		wantConsent []string
		wantChanges []string
		wantMode    string
	}{
		{
			name:        "unsubscribes by default",
			wantStatus:  domain.StatusUnsubscribed,
			wantConsent: []string{domain.ConsentUnsubscribed},
			wantChanges: []string{"unsubscribed"},
			wantMode:    DeleteUnsubscribe,
		},
		{
			name:        "hard delete erases personal data",
			mode:        DeleteHard,
			wantStatus:  domain.StatusErased,
			wantConsent: []string{domain.ConsentErased},
			wantChanges: []string{"erased"},
			wantMode:    DeleteHard,
		},
		{
			name: "erased subscriber is left alone",
			mode: DeleteHard,
			setup: func(f *fixture, sub domain.Subscriber) {
				sub.Status = domain.StatusErased
				f.repo.putSubscriber(sub)
			},
			wantStatus: domain.StatusErased,
			wantMode:   DeleteHard,
		},
		{name: "unknown mode", mode: "shred", wantErr: domain.ErrValidation},
		{
			name:    "subscriber lookup fails",
			setup:   func(f *fixture, _ domain.Subscriber) { f.repo.failOn("Subscriber", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "erase fails",
			mode:    DeleteHard,
			setup:   func(f *fixture, _ domain.Subscriber) { f.repo.failOn("EraseSubscriber", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "erase consent fails",
			mode:    DeleteHard,
			setup:   func(f *fixture, _ domain.Subscriber) { f.repo.failOn("AppendConsent", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "detail read fails",
			setup:   func(f *fixture, _ domain.Subscriber) { f.repo.failOn("ConsentHistory", errBoom) },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			sub := f.activeSubscriber("reader@example.test", "weekly")

			if tt.setup != nil {
				tt.setup(f, sub)
			}

			ctx, scope := audit.WithScope(t.Context())

			got, err := f.svc.DeleteSubscriber(ctx, testActor, sub.UUID, tt.mode)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Subscriber.Status)
			assert.Equal(t, tt.wantConsent, f.repo.consentKinds(sub.UUID))
			assert.Equal(t, tt.wantChanges, f.changes())

			for _, e := range f.repo.consentEvents() {
				assert.Equal(t, string(domain.SourceAdmin), e.Source)
			}

			for _, e := range f.events.Events() {
				assert.Equal(t, testActor, *e.ActorID)
			}

			entry := auditEntry(scope)
			assert.Equal(t, domain.ResourceSubscriber, entry.ResourceType)
			assert.Equal(t, sub.UUID, *entry.ResourceID)
			assert.Equal(t, tt.wantMode, entry.Metadata["mode"])
		})
	}
}

func TestEraseUser(t *testing.T) {
	t.Parallel()

	at := testNow.Add(time.Hour)

	tests := []struct {
		name       string
		setup      func(f *fixture)
		wantErr    error
		wantErased bool
	}{
		{
			name:       "erases the linked subscriber",
			setup:      func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly") },
			wantErased: true,
		},
		{name: "no linked subscriber"},
		{
			name: "already erased",
			setup: func(f *fixture) {
				linkedSubscriber(f, domain.StatusErased)
			},
		},
		{
			name:    "lookup fails",
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByUser", errBoom) },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			err := f.svc.EraseUser(t.Context(), testUser, at)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			if !tt.wantErased {
				assert.Zero(t, f.repo.callCount("EraseSubscriber"))
				assert.Empty(t, f.repo.consentEvents())

				return
			}

			sub := f.repo.onlySubscriber(t)
			assert.Equal(t, domain.StatusErased, sub.Status)
			assert.Equal(t, at, *sub.ErasedAt)
			assert.Empty(t, sub.Email)

			events := f.repo.consentEvents()
			require.Len(t, events, 1)
			assert.Equal(t, domain.ConsentErased, events[0].Event)
			assert.Equal(t, domain.ConsentSourcePrivacy, events[0].Source)
			assert.Equal(t, at, events[0].OccurredAt)

			recorded := f.events.Events()
			require.Len(t, recorded, 1)
			assert.Nil(t, recorded[0].ActorID)
			assert.Equal(t, []string{"erased"}, f.changes())
		})
	}
}

func sendRequests(f *fixture) []uuid.UUID {
	var out []uuid.UUID

	for _, e := range f.events.Events() {
		if e.Type == event.NewsletterIssueSendRequested {
			out = append(out, e.AggregateID)
		}
	}

	return out
}

func withoutPostalAddress(f *fixture) { f.repo.cfg.PostalAddress = "" }

func TestCreateIssue(t *testing.T) {
	t.Parallel()

	future := testNow.Add(time.Hour)
	past := testNow.Add(-time.Hour)
	base := IssueInput{Subject: "Hello", Preheader: "Hi", BodyMarkdown: "Body", Lists: []string{"weekly", "weekly"}}

	tests := []struct {
		name       string
		in         func(in *IssueInput)
		setup      func(f *fixture)
		wantErr    error
		wantStatus domain.IssueStatus
		check      func(t *testing.T, f *fixture, issue domain.Issue)
	}{
		{
			name:       "draft by default",
			wantStatus: domain.IssueDraft,
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				assert.Equal(t, []string{"weekly"}, issue.Lists)
				assert.Equal(t, testActor, *issue.CreatedBy)
				assert.Equal(t, testNow, issue.CreatedAt)
				assert.Empty(t, sendRequests(f))
			},
		},
		{
			name:       "queued hands it to the worker",
			in:         func(in *IssueInput) { in.Status = domain.IssueQueued },
			wantStatus: domain.IssueQueued,
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				assert.Equal(t, testNow, *issue.QueuedAt)
				assert.Equal(t, []uuid.UUID{issue.UUID}, sendRequests(f))
			},
		},
		{
			name:       "scheduled in the future",
			in:         func(in *IssueInput) { in.Status, in.SendAt = domain.IssueScheduled, &future },
			wantStatus: domain.IssueScheduled,
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				assert.Equal(t, future, *issue.SendAt)
				assert.Empty(t, sendRequests(f))
			},
		},
		{
			name:    "scheduled without a time",
			in:      func(in *IssueInput) { in.Status = domain.IssueScheduled },
			wantErr: domain.ErrValidation,
		},
		{
			name:    "scheduled in the past",
			in:      func(in *IssueInput) { in.Status, in.SendAt = domain.IssueScheduled, &past },
			wantErr: domain.ErrValidation,
		},
		{name: "sent is not a starting status", in: func(in *IssueInput) { in.Status = domain.IssueSent }, wantErr: domain.ErrValidation},
		{name: "unknown list", in: func(in *IssueInput) { in.Lists = []string{"nope"} }, wantErr: domain.ErrValidation},
		{name: "missing subject", in: func(in *IssueInput) { in.Subject = " " }, wantErr: domain.ErrValidation},
		{
			name:    "queued needs a postal address",
			in:      func(in *IssueInput) { in.Status = domain.IssueQueued },
			setup:   withoutPostalAddress,
			wantErr: domain.ErrNotConfigured,
		},
		{
			name:    "config unavailable",
			in:      func(in *IssueInput) { in.Status = domain.IssueQueued },
			setup:   func(f *fixture) { f.repo.failOn("Config", errBoom) },
			wantErr: errBoom,
		},
		{name: "lists unavailable", setup: func(f *fixture) { f.repo.failOn("Lists", errBoom) }, wantErr: errBoom},
		{name: "store fails", setup: func(f *fixture) { f.repo.failOn("CreateIssue", errBoom) }, wantErr: errBoom},
		{
			name:    "send request fails",
			in:      func(in *IssueInput) { in.Status = domain.IssueQueued },
			setup:   func(f *fixture) { f.events.Err = errBoom },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			in := base
			if tt.in != nil {
				tt.in(&in)
			}

			ctx, scope := audit.WithScope(t.Context())

			issue, err := f.svc.CreateIssue(ctx, testActor, in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, issue.Status)
			assert.Equal(t, issue, f.repo.storedIssue(issue.UUID))

			entry := auditEntry(scope)
			assert.Equal(t, domain.ResourceIssue, entry.ResourceType)
			assert.Equal(t, string(tt.wantStatus), entry.Metadata["status"])

			if tt.check != nil {
				tt.check(t, f, issue)
			}
		})
	}
}

func TestUpdateIssue(t *testing.T) {
	t.Parallel()

	future := testNow.Add(time.Hour)
	later := testNow.Add(2 * time.Hour)

	tests := []struct {
		name       string
		status     domain.IssueStatus
		sendAt     *time.Time
		patch      IssuePatch
		setup      func(f *fixture)
		wantErr    error
		wantStatus domain.IssueStatus
		wantMove   []string
		check      func(t *testing.T, f *fixture, issue domain.Issue)
	}{
		{
			name:   "edit a draft",
			status: domain.IssueDraft,
			patch: IssuePatch{
				Subject: new("New subject"), Preheader: new("New preheader"), BodyMarkdown: new("New body"),
				Lists: &[]string{"product"},
			},
			wantStatus: domain.IssueDraft,
			check: func(t *testing.T, _ *fixture, issue domain.Issue) {
				assert.Equal(t, "New subject", issue.Subject)
				assert.Equal(t, "New preheader", issue.Preheader)
				assert.Equal(t, "New body", issue.BodyMarkdown)
				assert.Equal(t, []string{"product"}, issue.Lists)
				assert.Equal(t, testActor, *issue.UpdatedBy)
				assert.Equal(t, testNow, issue.UpdatedAt)
			},
		},
		{
			name:       "an empty patch still saves",
			status:     domain.IssueDraft,
			wantStatus: domain.IssueDraft,
		},
		{
			name:       "schedule a draft",
			status:     domain.IssueDraft,
			patch:      IssuePatch{Status: new(domain.IssueScheduled), SendAt: &future},
			wantStatus: domain.IssueScheduled,
			wantMove:   []string{"draft", "scheduled"},
			check: func(t *testing.T, _ *fixture, issue domain.Issue) {
				assert.Equal(t, future, *issue.SendAt)
			},
		},
		{
			name:       "reschedule",
			status:     domain.IssueScheduled,
			sendAt:     &future,
			patch:      IssuePatch{SendAt: &later},
			wantStatus: domain.IssueScheduled,
			wantMove:   []string{"scheduled", "scheduled"},
			check: func(t *testing.T, _ *fixture, issue domain.Issue) {
				assert.Equal(t, later, *issue.SendAt)
			},
		},
		{
			name:       "back to draft clears the send time",
			status:     domain.IssueScheduled,
			sendAt:     &future,
			patch:      IssuePatch{Status: new(domain.IssueDraft)},
			wantStatus: domain.IssueDraft,
			wantMove:   []string{"scheduled", "draft"},
			check: func(t *testing.T, _ *fixture, issue domain.Issue) {
				assert.Nil(t, issue.SendAt)
			},
		},
		{
			name:       "queue a draft",
			status:     domain.IssueDraft,
			patch:      IssuePatch{Status: new(domain.IssueQueued)},
			wantStatus: domain.IssueQueued,
			wantMove:   []string{"draft", "queued"},
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				assert.Equal(t, testNow, *issue.QueuedAt)
				assert.Equal(t, []uuid.UUID{issue.UUID}, sendRequests(f))
			},
		},
		{
			name:       "resume a sending issue",
			status:     domain.IssueSending,
			patch:      IssuePatch{Status: new(domain.IssueQueued)},
			wantStatus: domain.IssueQueued,
			wantMove:   []string{"sending", "queued"},
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				assert.Equal(t, []uuid.UUID{issue.UUID}, sendRequests(f))
				assert.Zero(t, f.repo.callCount("UpdateIssue"), "only the status moves")
			},
		},
		{
			name:       "cancel a queued issue",
			status:     domain.IssueQueued,
			patch:      IssuePatch{Status: new(domain.IssueCancelled)},
			wantStatus: domain.IssueCancelled,
			wantMove:   []string{"queued", "cancelled"},
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Empty(t, sendRequests(f))
			},
		},
		{
			name:    "content of a queued issue is frozen",
			status:  domain.IssueQueued,
			patch:   IssuePatch{Subject: new("Late edit")},
			wantErr: domain.ErrConflict,
		},
		{name: "unknown list", status: domain.IssueDraft, patch: IssuePatch{Lists: &[]string{"nope"}}, wantErr: domain.ErrValidation},
		{name: "invalid subject", status: domain.IssueDraft, patch: IssuePatch{Subject: new("")}, wantErr: domain.ErrValidation},
		{
			name: "send time needs scheduling", status: domain.IssueDraft,
			patch: IssuePatch{SendAt: &future}, wantErr: domain.ErrValidation,
		},
		{
			name: "forbidden transition", status: domain.IssueDraft,
			patch: IssuePatch{Status: new(domain.IssueSent)}, wantErr: domain.ErrConflict,
		},
		{
			name: "queue needs a postal address", status: domain.IssueDraft,
			patch: IssuePatch{Status: new(domain.IssueQueued)}, setup: withoutPostalAddress, wantErr: domain.ErrNotConfigured,
		},
		{
			name: "status changed concurrently", status: domain.IssueDraft,
			patch: IssuePatch{Subject: new("Edit")},
			setup: func(f *fixture) {
				f.repo.hook("UpdateIssue", func(r *memRepo) {
					for id, issue := range r.issues {
						issue.Status = domain.IssueQueued
						r.issues[id] = issue
					}
				})
			},
			wantErr: domain.ErrConflict,
		},
		{
			name: "status moved concurrently", status: domain.IssueSending,
			patch: IssuePatch{Status: new(domain.IssueCancelled)},
			setup: func(f *fixture) {
				f.repo.hook("MoveIssue", func(r *memRepo) {
					for id, issue := range r.issues {
						issue.Status = domain.IssueSent
						r.issues[id] = issue
					}
				})
			},
			wantErr: domain.ErrConflict,
		},
		{
			name: "save fails", status: domain.IssueDraft,
			setup:   func(f *fixture) { f.repo.failOn("UpdateIssue", errBoom) },
			wantErr: errBoom,
		},
		{
			name: "missing issue", status: domain.IssueDraft,
			setup:   func(f *fixture) { f.repo.failOn("Issue", domain.ErrNotFound) },
			wantErr: domain.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			issue := domain.Issue{
				UUID: uuid.New(), Subject: "Hello", BodyMarkdown: "Body", Lists: []string{"weekly"},
				Status: tt.status, SendAt: tt.sendAt, CreatedAt: testNow.Add(-time.Hour),
			}
			f.repo.putIssue(issue)

			if tt.setup != nil {
				tt.setup(f)
			}

			ctx, scope := audit.WithScope(t.Context())

			got, err := f.svc.UpdateIssue(ctx, testActor, issue.UUID, tt.patch)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, got, f.repo.storedIssue(issue.UUID))

			entry := auditEntry(scope)
			assert.Equal(t, issue.UUID, *entry.ResourceID)

			if tt.wantMove != nil {
				assert.Equal(t, auditdomain.Change{From: tt.wantMove[0], To: tt.wantMove[1]}, entry.Changes["status"])
			} else {
				assert.NotContains(t, entry.Changes, "status")
			}

			if tt.check != nil {
				tt.check(t, f, got)
			}
		})
	}
}

func TestIssue(t *testing.T) {
	t.Parallel()

	f := newFixture()
	issue := domain.Issue{UUID: uuid.New(), Subject: "Hello", Status: domain.IssueDraft}
	f.repo.putIssue(issue)

	got, err := f.svc.Issue(t.Context(), issue.UUID)
	require.NoError(t, err)
	require.Equal(t, issue, got)

	_, err = f.svc.Issue(t.Context(), uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestListIssues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filter      domain.IssueFilter
		setup       func(f *fixture)
		wantStatus  domain.IssueStatus
		wantPage    int
		wantPerPage int
		wantErr     error
	}{
		{name: "defaults the page", wantPage: 1, wantPerPage: defaultPerPage},
		{
			name:        "caps the page size",
			filter:      domain.IssueFilter{Status: domain.IssueSent, Page: 2, PerPage: 1000},
			wantStatus:  domain.IssueSent,
			wantPage:    2,
			wantPerPage: maxPerPage,
		},
		{name: "unknown status", filter: domain.IssueFilter{Status: "lost"}, wantErr: domain.ErrValidation},
		{name: "store fails", setup: func(f *fixture) { f.repo.failOn("ListIssues", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			got, err := f.svc.ListIssues(t.Context(), tt.filter)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, f.repo.issueFilter.Status)
			require.Equal(t, tt.wantPage, f.repo.issueFilter.Page)
			require.Equal(t, tt.wantPerPage, f.repo.issueFilter.PerPage)
			require.Equal(t, tt.wantPerPage, got.OffsetPerPage)
			require.Equal(t, tt.wantPerPage, got.Limit)
		})
	}
}
