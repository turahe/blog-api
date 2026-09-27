package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

func scheduledIssue(f *fixture, sendAt time.Time) domain.Issue {
	issue := domain.Issue{
		UUID: uuid.New(), Subject: "Hello", BodyMarkdown: "Body", Lists: []string{"weekly"},
		Status: domain.IssueScheduled, SendAt: &sendAt,
	}
	f.repo.putIssue(issue)

	return issue
}

func TestReleaseDue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		limit        int
		setup        func(f *fixture)
		wantReleased int
		wantErr      error
	}{
		{name: "queues every due issue", limit: 10, wantReleased: 2},
		{name: "respects the limit", limit: 1, wantReleased: 1},
		{
			name: "an issue moved meanwhile is skipped", limit: 10,
			setup: func(f *fixture) {
				f.repo.hook("MoveIssue", func(r *memRepo) {
					for id, issue := range r.issues {
						if !issue.SendAt.After(testNow) {
							issue.Status = domain.IssueCancelled
							r.issues[id] = issue
						}
					}
				})
			},
		},
		{name: "due lookup fails", limit: 10, setup: func(f *fixture) { f.repo.failOn("DueIssues", errBoom) }, wantErr: errBoom},
		{name: "move fails", limit: 10, setup: func(f *fixture) { f.repo.failOn("MoveIssue", errBoom) }, wantErr: errBoom},
		{
			name: "send request fails after the first", limit: 10,
			setup:        func(f *fixture) { f.events.Err = errBoom },
			wantReleased: 1,
			wantErr:      errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			first := scheduledIssue(f, testNow.Add(-time.Hour))
			second := scheduledIssue(f, testNow)
			later := scheduledIssue(f, testNow.Add(time.Minute))

			if tt.setup != nil {
				tt.setup(f)
			}

			released, err := f.svc.ReleaseDue(t.Context(), tt.limit)
			assert.Equal(t, tt.wantReleased, released)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, domain.IssueScheduled, f.repo.storedIssue(later.UUID).Status)

			requests := sendRequests(f)
			require.Len(t, requests, tt.wantReleased)

			for i, id := range requests {
				assert.Equal(t, []uuid.UUID{first.UUID, second.UUID}[i], id, "earliest first")
				assert.Equal(t, domain.IssueQueued, f.repo.storedIssue(id).Status)
			}
		})
	}
}

func dispatchIssue(f *fixture, status domain.IssueStatus) domain.Issue {
	issue := domain.Issue{
		UUID: uuid.New(), Subject: "Launch notes", Preheader: "What shipped",
		BodyMarkdown: "Hello <script>x()</script>world", Lists: []string{"weekly"}, Status: status,
	}
	f.repo.putIssue(issue)

	return issue
}

func recipients(n int) []domain.Recipient {
	out := make([]domain.Recipient, n)
	for i := range out {
		out[i] = domain.Recipient{
			DeliveryID: uuid.New(), SubscriberID: uuid.New(), Email: fmt.Sprintf("r%d@example.test", i), Format: domain.FormatHTML,
		}
	}

	return out
}

func TestDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		edit       func(*Deps, *Config)
		status     domain.IssueStatus
		recipients int
		setup      func(f *fixture, issue domain.Issue)
		wantErr    error
		wantStatus domain.IssueStatus
		wantSent   int
		check      func(t *testing.T, f *fixture, issue domain.Issue)
	}{
		{
			name: "delivers a queued issue", status: domain.IssueQueued, recipients: 2,
			setup: func(f *fixture, issue domain.Issue) {
				f.repo.mu.Lock()
				q := f.repo.queue[issue.UUID]
				q[1].Format = domain.FormatPlaintext
				f.repo.mu.Unlock()
			},
			wantStatus: domain.IssueSent, wantSent: 2,
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				emails := f.sender.sent()
				deliveries := f.repo.recordedDeliveries()
				require.Len(t, deliveries, 2)

				html := emails[0]
				assert.Equal(t, "r0@example.test", html.To)
				assert.Equal(t, "Launch notes", html.Subject)
				assert.Equal(t, "Example", html.FromName)
				assert.Equal(t, "news@example.test", html.FromEmail)
				assert.Equal(t, "reply@example.test", html.ReplyTo)
				assert.Equal(t, deliveries[0].DeliveryID.String(), html.IdempotencyKey)
				assert.Equal(t, "List-Unsubscribe=One-Click", html.Headers["List-Unsubscribe-Post"])
				assert.True(t, strings.HasPrefix(html.Headers["List-Unsubscribe"],
					"<https://api.example.test/api/v1/newsletter/unsubscribe?token="))
				assert.Contains(t, html.HTML, "<p>Hello <script>x()</script>world</p>", "the rendered markdown is the body")
				assert.Contains(t, html.HTML, "What shipped")
				assert.Contains(t, html.HTML, "https://blog.example.test/newsletter/unsubscribe?token=")
				assert.Contains(t, html.Text, "Launch notes\n\nHello world\n\n--\n")
				assert.Contains(t, html.Text, "subscribed to Example Blog.")
				assert.Contains(t, html.Text, "\n1 Example Street\n")
				assert.Empty(t, emails[1].HTML, "plaintext subscribers get no HTML part")

				token, err := f.svc.lookupToken(t.Context(), unsubscribeToken(t, html), domain.PurposeUnsubscribe)
				require.NoError(t, err)
				assert.Equal(t, issue.UUID, *token.IssueID)
				assert.Equal(t, testNow.Add(DefaultLinkTTL), token.ExpiresAt)
				assert.Len(t, f.repo.tokensFor(token.SubscriberID, domain.PurposePreferences), 1)

				f.repo.mu.Lock()
				claim := f.repo.claims[0]
				f.repo.mu.Unlock()
				assert.Equal(t, domain.Claim{
					Limit: DefaultBatchSize, MaxAttempts: DefaultMaxAttempts, Now: testNow,
					StaleBefore: testNow.Add(-DefaultClaimTimeout), RetryBefore: testNow,
				}, claim)

				assert.Contains(t, f.logs.String(), "newsletter issue sent")
			},
		},
		{name: "resumes a sending issue", status: domain.IssueSending, recipients: 1, wantStatus: domain.IssueSent, wantSent: 1},
		{
			name: "claims in batches", status: domain.IssueQueued, recipients: 3,
			edit:       func(_ *Deps, c *Config) { c.BatchSize = 1 },
			wantStatus: domain.IssueSent, wantSent: 3,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Equal(t, 4, f.repo.callCount("ClaimRecipients"), "three batches and an empty claim")
			},
		},
		{name: "drafts are not sent", status: domain.IssueDraft, recipients: 1, wantStatus: domain.IssueDraft},
		{name: "scheduled issues wait", status: domain.IssueScheduled, recipients: 1, wantStatus: domain.IssueScheduled},
		{name: "sent issues are not resent", status: domain.IssueSent, recipients: 1, wantStatus: domain.IssueSent},
		{name: "cancelled issues stop", status: domain.IssueCancelled, recipients: 1, wantStatus: domain.IssueCancelled},
		{
			name: "another worker took it", status: domain.IssueQueued, recipients: 1,
			setup: func(f *fixture, _ domain.Issue) {
				f.repo.hook("MoveIssue", func(r *memRepo) {
					for id, issue := range r.issues {
						issue.Status = domain.IssueCancelled
						r.issues[id] = issue
					}
				})
			},
			wantStatus: domain.IssueCancelled,
		},
		{
			name: "cancelled while sending", status: domain.IssueQueued, recipients: 2,
			edit: func(_ *Deps, c *Config) { c.BatchSize = 1 },
			setup: func(f *fixture, issue domain.Issue) {
				f.sender.fn = func(ports.Email) error {
					cancelled := f.repo.storedIssue(issue.UUID)
					cancelled.Status = domain.IssueCancelled
					f.repo.putIssue(cancelled)

					return nil
				}
			},
			wantStatus: domain.IssueCancelled, wantSent: 1,
		},
		{
			name: "without markdown the body is empty", status: domain.IssueQueued, recipients: 1,
			edit:       func(d *Deps, _ *Config) { d.Markdown = nil },
			wantStatus: domain.IssueSent, wantSent: 1,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.NotContains(t, f.sender.sent()[0].HTML, "Hello")
			},
		},
		{
			name: "without links there is no one-click header", status: domain.IssueQueued, recipients: 1,
			edit:       func(d *Deps, _ *Config) { d.Links = nil },
			wantStatus: domain.IssueSent, wantSent: 1,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				email := f.sender.sent()[0]
				assert.NotContains(t, email.Headers, "List-Unsubscribe")
				assert.Contains(t, email.Text, "subscribed to Example.\nUnsubscribe: /newsletter/unsubscribe?token=")
			},
		},
		{
			name: "permanent rejection counts as failed", status: domain.IssueQueued, recipients: 1,
			setup: func(f *fixture, _ domain.Issue) {
				f.sender.fn = func(ports.Email) error { return fmt.Errorf("%w: mailbox gone", domain.ErrPermanent) }
			},
			wantStatus: domain.IssueSent,
			check: func(t *testing.T, f *fixture, issue domain.Issue) {
				d := f.repo.recordedDeliveries()[0]
				assert.True(t, d.Permanent)
				assert.False(t, d.Sent)
				assert.Contains(t, d.Error, "mailbox gone")
				assert.Equal(t, 1, f.repo.storedIssue(issue.UUID).FailedCount)
			},
		},
		{
			name: "transient failure asks for a retry", status: domain.IssueQueued, recipients: 1,
			setup: func(f *fixture, _ domain.Issue) {
				f.sender.fn = func(ports.Email) error { return errBoom }
			},
			wantErr:    domain.ErrDeliveriesPending,
			wantStatus: domain.IssueSending,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				d := f.repo.recordedDeliveries()[0]
				assert.False(t, d.Permanent)
				assert.Equal(t, "boom", d.Error)
			},
		},
		{
			name: "long provider errors are truncated", status: domain.IssueQueued, recipients: 1,
			setup: func(f *fixture, _ domain.Issue) {
				f.sender.fn = func(ports.Email) error { return errors.New(strings.Repeat("x", 2*maxErrorLength)) }
			},
			wantErr:    domain.ErrDeliveriesPending,
			wantStatus: domain.IssueSending,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Len(t, f.repo.recordedDeliveries()[0].Error, maxErrorLength)
			},
		},
		{
			name: "unsubscribe link fails", status: domain.IssueQueued, recipients: 1,
			setup:      func(f *fixture, _ domain.Issue) { f.repo.failOn("CreateToken", errBoom) },
			wantErr:    domain.ErrDeliveriesPending,
			wantStatus: domain.IssueSending,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Empty(t, f.sender.sent())
				assert.Equal(t, "boom", f.repo.recordedDeliveries()[0].Error)
			},
		},
		{
			name: "preferences link fails", status: domain.IssueQueued, recipients: 1,
			setup:      func(f *fixture, _ domain.Issue) { f.repo.failAt("CreateToken", 2, errBoom) },
			wantErr:    domain.ErrDeliveriesPending,
			wantStatus: domain.IssueSending,
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Empty(t, f.sender.sent())
			},
		},
		{
			name: "missing issue is dropped", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("Issue", domain.ErrNotFound) },
			check: func(t *testing.T, f *fixture, _ domain.Issue) {
				assert.Contains(t, f.logs.String(), "dispatch for a missing issue")
			},
			wantStatus: domain.IssueQueued,
		},
		{name: "without a sender", status: domain.IssueQueued, edit: func(d *Deps, _ *Config) { d.Sender = nil }, wantErr: domain.ErrNotConfigured},
		{
			name: "issue load fails", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("Issue", errBoom) }, wantErr: errBoom,
		},
		{
			name: "start fails", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("MoveIssue", errBoom) }, wantErr: errBoom,
		},
		{
			name: "config unavailable", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("Config", errBoom) }, wantErr: errBoom,
		},
		{
			name: "reload fails", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failAt("Issue", 2, errBoom) }, wantErr: errBoom,
		},
		{
			name: "claim fails", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("ClaimRecipients", errBoom) }, wantErr: errBoom,
		},
		{
			name: "recording a delivery fails", status: domain.IssueQueued, recipients: 1,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("RecordDelivery", errBoom) }, wantErr: errBoom,
		},
		{
			name: "counts unavailable", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("DeliveryCounts", errBoom) }, wantErr: errBoom,
		},
		{
			name: "finish fails", status: domain.IssueQueued,
			setup: func(f *fixture, _ domain.Issue) { f.repo.failOn("FinishIssue", errBoom) }, wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var edits []func(*Deps, *Config)
			if tt.edit != nil {
				edits = append(edits, tt.edit)
			}

			f := newFixture(edits...)
			issue := dispatchIssue(f, tt.status)
			f.repo.enqueue(issue.UUID, recipients(tt.recipients)...)

			if tt.setup != nil {
				tt.setup(f, issue)
			}

			err := f.svc.Dispatch(t.Context(), issue.UUID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			if tt.wantStatus == "" {
				return
			}

			stored := f.repo.storedIssue(issue.UUID)
			assert.Equal(t, tt.wantStatus, stored.Status)
			assert.Equal(t, tt.wantSent, countSent(f))

			if tt.wantStatus == domain.IssueSent {
				assert.Equal(t, tt.wantSent, stored.SentCount)
			}

			if tt.check != nil {
				tt.check(t, f, issue)
			}
		})
	}
}

func countSent(f *fixture) int {
	n := 0

	for _, d := range f.repo.recordedDeliveries() {
		if d.Sent {
			n++
		}
	}

	return n
}

func TestSyncSubscriber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		edit     func(*Deps, *Config)
		missing  bool
		setup    func(f *fixture, contacts *fakeContacts)
		wantErr  error
		wantSync bool
	}{
		{name: "pushes the current state", wantSync: true},
		{name: "without a contact store", edit: func(d *Deps, _ *Config) { d.Contacts = nil }},
		{name: "missing subscriber", missing: true},
		{
			name:    "lookup fails",
			setup:   func(f *fixture, _ *fakeContacts) { f.repo.failOn("Subscriber", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "provider fails",
			setup:   func(_ *fixture, c *fakeContacts) { c.err = errBoom },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			contacts := &fakeContacts{}
			edits := []func(*Deps, *Config){func(d *Deps, _ *Config) { d.Contacts = contacts }}

			if tt.edit != nil {
				edits = append(edits, tt.edit)
			}

			f := newFixture(edits...)
			sub := f.activeSubscriber("reader@example.test", "weekly", "product")

			id := sub.UUID
			if tt.missing {
				id = uuid.New()
			}

			if tt.setup != nil {
				tt.setup(f, contacts)
			}

			err := f.svc.SyncSubscriber(t.Context(), id)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			if !tt.wantSync {
				require.Empty(t, contacts.contacts)
				return
			}

			require.Equal(t, []ports.Contact{{
				ID: sub.UUID, Email: "reader@example.test", Status: domain.StatusActive, Format: domain.FormatHTML,
				Lists: []string{"weekly", "product"}, ChangedAt: testNow,
			}}, contacts.contacts)
		})
	}
}

func TestHandleFeedback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fb          domain.ProviderFeedback
		setup       func(f *fixture)
		wantErr     error
		wantStatus  domain.Status
		wantConsent []string
		wantChanges []string
	}{
		{
			name:        "soft bounce is only recorded",
			fb:          domain.ProviderFeedback{Kind: domain.BounceSoft, Email: "Reader@Example.test"},
			wantStatus:  domain.StatusActive,
			wantConsent: []string{domain.ConsentSoftBounce},
		},
		{
			name:        "hard bounce suppresses",
			fb:          domain.ProviderFeedback{Kind: domain.BounceHard, Email: "reader@example.test"},
			wantStatus:  domain.StatusBounced,
			wantConsent: []string{domain.ConsentBounced},
			wantChanges: []string{"bounced"},
		},
		{
			name:        "complaint suppresses",
			fb:          domain.ProviderFeedback{Kind: domain.Complaint, Email: "reader@example.test"},
			wantStatus:  domain.StatusComplained,
			wantConsent: []string{domain.ConsentComplained},
			wantChanges: []string{"complained"},
		},
		{
			name:       "unknown address is ignored",
			fb:         domain.ProviderFeedback{Kind: domain.BounceHard, Email: "other@example.test"},
			wantStatus: domain.StatusActive,
		},
		{name: "invalid email", fb: domain.ProviderFeedback{Kind: domain.BounceHard, Email: "nope"}, wantErr: domain.ErrValidation},
		{name: "unknown kind", fb: domain.ProviderFeedback{Kind: "delayed", Email: "reader@example.test"}, wantErr: domain.ErrValidation},
		{
			name:    "lookup fails",
			fb:      domain.ProviderFeedback{Kind: domain.BounceHard, Email: "reader@example.test"},
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByEmail", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "revoke fails",
			fb:      domain.ProviderFeedback{Kind: domain.Complaint, Email: "reader@example.test"},
			setup:   func(f *fixture) { f.repo.failOn("RevokeTokens", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "save fails",
			fb:      domain.ProviderFeedback{Kind: domain.BounceHard, Email: "reader@example.test"},
			setup:   func(f *fixture) { f.repo.failOn("UpdateSubscriber", errBoom) },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			sub := f.activeSubscriber("reader@example.test", "weekly")
			f.token(sub.UUID, domain.PurposeConfirm)

			if tt.setup != nil {
				tt.setup(f)
			}

			err := f.svc.HandleFeedback(t.Context(), tt.fb)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			got := f.repo.subscriber(t, sub.UUID)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantConsent, f.repo.consentKinds(sub.UUID))
			assert.Equal(t, tt.wantChanges, f.changes())

			for _, e := range f.repo.consentEvents() {
				assert.Equal(t, domain.ConsentSourceProvider, e.Source)
			}

			suppressed := tt.wantChanges != nil
			assert.Equal(t, suppressed, len(f.repo.tokensFor(sub.UUID, domain.PurposeConfirm)) == 0,
				"suppression revokes pending confirmations")
			assert.Equal(t, tt.wantStatus == domain.StatusBounced, got.BouncedAt != nil)
			assert.Equal(t, tt.wantStatus == domain.StatusComplained, got.ComplainedAt != nil)
		})
	}
}

func TestProviderWebhook(t *testing.T) {
	t.Parallel()

	events := func(entries ...string) []byte {
		return []byte(`{"events":[` + strings.Join(entries, ",") + `]}`)
	}
	hard := `{"type":"hard_bounce","email":"reader@example.test","occurred_at":"2026-09-27T11:00:00Z"}`
	soft := `{"type":"soft_bounce","email":"other@example.test"}`

	tests := []struct {
		name        string
		edit        func(*Deps, *Config)
		body        []byte
		setup       func(f *fixture)
		wantApplied int
		wantErr     error
		wantStatus  domain.Status
	}{
		{name: "applies the batch", body: events(hard, soft), wantApplied: 2, wantStatus: domain.StatusBounced},
		{
			name: "without a verifier", body: events(hard),
			edit:    func(d *Deps, _ *Config) { d.Webhooks = nil },
			wantErr: domain.ErrNotConfigured, wantStatus: domain.StatusActive,
		},
		{
			name: "bad signature", body: events(hard),
			edit:    func(d *Deps, _ *Config) { d.Webhooks = fakeVerifier{err: errBoom} },
			wantErr: domain.ErrSignature, wantStatus: domain.StatusActive,
		},
		{name: "not JSON", body: []byte("nope"), wantErr: domain.ErrValidation, wantStatus: domain.StatusActive},
		{name: "no events", body: events(), wantErr: domain.ErrValidation, wantStatus: domain.StatusActive},
		{
			name: "too many events", body: events(slices.Repeat([]string{soft}, maxWebhookEvents+1)...),
			wantErr: domain.ErrValidation, wantStatus: domain.StatusActive,
		},
		{
			name: "a bad type rejects the whole batch", body: events(hard, `{"type":"opened","email":"a@example.test"}`),
			wantErr: domain.ErrValidation, wantStatus: domain.StatusActive,
		},
		{
			name: "a bad email rejects the whole batch", body: events(hard, `{"type":"complaint","email":"nope"}`),
			wantErr: domain.ErrValidation, wantStatus: domain.StatusActive,
		},
		{
			name: "stops at the failing event", body: events(hard, soft),
			setup:       func(f *fixture) { f.repo.failAt("SubscriberByEmail", 2, errBoom) },
			wantApplied: 1, wantErr: errBoom, wantStatus: domain.StatusBounced,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			edits := []func(*Deps, *Config){func(d *Deps, _ *Config) { d.Webhooks = fakeVerifier{} }}
			if tt.edit != nil {
				edits = append(edits, tt.edit)
			}

			f := newFixture(edits...)
			sub := f.activeSubscriber("reader@example.test", "weekly")

			if tt.setup != nil {
				tt.setup(f)
			}

			applied, err := f.svc.ProviderWebhook(t.Context(), WebhookInput{Timestamp: "1", Signature: "sig", Body: tt.body})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantApplied, applied)
			assert.Equal(t, tt.wantStatus, f.repo.subscriber(t, sub.UUID).Status)
		})
	}
}

func TestPruneTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(f *fixture)
		wantPruned int64
		wantErr    error
	}{
		{name: "deletes tokens expired before the retention", wantPruned: 1},
		{name: "store fails", setup: func(f *fixture) { f.repo.failOn("PruneTokens", errBoom) }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			f.repo.putToken("old", domain.Token{ExpiresAt: testNow.Add(-10 * 24 * time.Hour)})
			f.repo.putToken("recent", domain.Token{ExpiresAt: testNow.Add(-24 * time.Hour)})

			if tt.setup != nil {
				tt.setup(f)
			}

			pruned, err := f.svc.PruneTokens(t.Context(), 7*24*time.Hour)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantPruned, pruned)
			require.Equal(t, testNow.Add(-7*24*time.Hour), f.repo.prunedBefore)
		})
	}
}

func TestSubscriberEvent(t *testing.T) {
	t.Parallel()

	sub := domain.Subscriber{UUID: uuid.New(), Email: "reader@example.test", Status: domain.StatusBounced}
	at := time.Date(2026, 9, 27, 19, 0, 0, 0, time.FixedZone("WIB", 7*60*60))

	got := subscriberEvent(sub, "bounced", &testActor, at)

	require.Equal(t, event.NewsletterSubscriberChanged, got.Type)
	require.Equal(t, event.AggregateNewsletterSubscriber, got.AggregateType)
	require.Equal(t, sub.UUID, got.AggregateID)
	require.Equal(t, testActor, *got.ActorID)
	require.Equal(t, map[string]any{
		"subscriber_id": sub.UUID.String(), "status": "bounced", "change": "bounced", "occurred_at": at.UTC(),
	}, got.Payload, "the payload carries ids only, never the address")
}
