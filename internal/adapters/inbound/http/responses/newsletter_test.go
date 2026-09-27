package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
)

var (
	nlSubscriberID = uuid.MustParse("0198a1b2-0000-7000-8000-0000000e0001")
	nlUserID       = uuid.MustParse("0198a1b2-0000-7000-8000-0000000e0002")
	nlIssueID      = uuid.MustParse("0198a1b2-0000-7000-8000-0000000e0003")
	nlStaffID      = uuid.MustParse("0198a1b2-0000-7000-8000-0000000e0004")
	nlJoinedAt     = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	nlLeftAt       = time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
)

func nlLists() []nldomain.List {
	return []nldomain.List{
		{Slug: "weekly", Name: "Weekly", Description: "Digest", IsDefault: true, Position: 1},
		{Slug: "launches", Name: "Launches", Position: 2, ArchivedAt: &nlLeftAt},
	}
}

const nlListsJSON = `[
	{"slug": "weekly", "name": "Weekly", "description": "Digest", "isDefault": true, "position": 1, "archivedAt": null},
	{"slug": "launches", "name": "Launches", "description": "", "isDefault": false, "position": 2,
	 "archivedAt": "2026-08-03T10:00:00Z"}
]`

// activeSubscriber is in weekly and has left launches.
func activeSubscriber() nldomain.Subscriber {
	return nldomain.Subscriber{
		UUID:        nlSubscriberID,
		Email:       "reader@example.com",
		DisplayName: "Rea",
		UserID:      &nlUserID,
		Status:      nldomain.StatusActive,
		Format:      nldomain.FormatHTML,
		Source:      nldomain.SourceAccount,
		IPHash:      "iphash",
		OptedInAt:   &nlJoinedAt,
		CreatedAt:   time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:   nlLeftAt,
		Memberships: []nldomain.Membership{
			{ListSlug: "weekly", ListName: "Weekly", State: nldomain.MembershipActive, JoinedAt: &nlJoinedAt},
			{ListSlug: "launches", ListName: "Launches", State: nldomain.MembershipLeft, JoinedAt: &nlJoinedAt, LeftAt: &nlLeftAt},
		},
	}
}

const activeMembershipsJSON = `[
	{"slug": "weekly", "name": "Weekly", "state": "active", "joinedAt": "2026-08-01T10:00:00Z", "leftAt": null},
	{"slug": "launches", "name": "Launches", "state": "left", "joinedAt": "2026-08-01T10:00:00Z",
	 "leftAt": "2026-08-03T10:00:00Z"}
]`

const activeSubscriberJSON = `"id": "0198a1b2-0000-7000-8000-0000000e0001", "email": "reader@example.com",
	"displayName": "Rea", "userId": "0198a1b2-0000-7000-8000-0000000e0002", "status": "active", "format": "html",
	"source": "account", "memberships": ` + activeMembershipsJSON + `,
	"optedInAt": "2026-08-01T10:00:00Z", "unsubscribedAt": null, "bouncedAt": null, "complainedAt": null,
	"erasedAt": null, "createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-03T10:00:00Z"`

func TestNewsletterList(t *testing.T) {
	t.Parallel()

	assertJSON(t, `{"slug": "weekly", "name": "Weekly", "description": "Digest", "isDefault": true,
		"position": 1, "archivedAt": null}`, NewsletterList(nlLists()[0]))
}

func TestNewsletterLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lists []nldomain.List
		want  string
	}{
		{name: "none", lists: nil, want: `[]`},
		{name: "in order", lists: nlLists(), want: nlListsJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterLists(tt.lists))
		})
	}
}

func TestNewsletterStatus(t *testing.T) {
	t.Parallel()

	assertJSON(t, `{"status": "pending_confirm"}`, NewsletterStatus("pending_confirm"))
}

func TestNewsletterConfirmed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sub  nldomain.Subscriber
		want string
	}{
		{
			name: "active lists only",
			sub:  activeSubscriber(),
			want: `{"status": "active", "lists": ["weekly"], "optedInAt": "2026-08-01T10:00:00Z"}`,
		},
		{
			name: "no active list",
			sub:  nldomain.Subscriber{Status: nldomain.StatusPending},
			want: `{"status": "pending_confirm", "lists": [], "optedInAt": null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterConfirmed(tt.sub))
		})
	}
}

func TestNewsletterMine(t *testing.T) {
	t.Parallel()

	pending := activeSubscriber()
	pending.Status = nldomain.StatusPending

	unsubscribed := activeSubscriber()
	unsubscribed.Status = nldomain.StatusUnsubscribed
	unsubscribed.UnsubscribedAt = &nlLeftAt

	const preferences = `"email": "reader@example.com", "format": "html", "memberships": ` + activeMembershipsJSON + `,
		"availableLists": ` + nlListsJSON + `, "optedInAt": "2026-08-01T10:00:00Z"`

	tests := []struct {
		name  string
		sub   nldomain.Subscriber
		found bool
		want  string
	}{
		{
			name:  "no subscription",
			sub:   nldomain.Subscriber{},
			found: false,
			want: `{"subscribed": false, "status": null, "format": null, "memberships": [],
				"availableLists": ` + nlListsJSON + `}`,
		},
		{
			name:  "active",
			sub:   activeSubscriber(),
			found: true,
			want:  `{` + preferences + `, "status": "active", "unsubscribedAt": null, "subscribed": true}`,
		},
		{
			name:  "pending confirmation counts as subscribed",
			sub:   pending,
			found: true,
			want:  `{` + preferences + `, "status": "pending_confirm", "unsubscribedAt": null, "subscribed": true}`,
		},
		{
			name:  "unsubscribed",
			sub:   unsubscribed,
			found: true,
			want:  `{` + preferences + `, "status": "unsubscribed", "unsubscribedAt": "2026-08-03T10:00:00Z", "subscribed": false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterMine(tt.sub, nlLists(), tt.found))
		})
	}
}

func TestNewsletterPreferences(t *testing.T) {
	t.Parallel()

	const rest = `"status": "active", "format": "html", "memberships": ` + activeMembershipsJSON + `,
		"availableLists": [], "optedInAt": "2026-08-01T10:00:00Z", "unsubscribedAt": null`

	tests := []struct {
		name string
		mask bool
		want string
	}{
		{name: "owner sees the address", mask: false, want: `{"email": "reader@example.com", ` + rest + `}`},
		{name: "token page masks the address", mask: true, want: `{"email": "r*****@example.com", ` + rest + `}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterPreferences(activeSubscriber(), nil, tt.mask))
		})
	}
}

func TestNewsletterSubscriber(t *testing.T) {
	t.Parallel()

	erased := nldomain.Subscriber{
		UUID:           nlSubscriberID,
		Status:         nldomain.StatusErased,
		Format:         nldomain.FormatPlaintext,
		Source:         nldomain.SourcePublic,
		UnsubscribedAt: &nlJoinedAt,
		BouncedAt:      &nlJoinedAt,
		ComplainedAt:   &nlJoinedAt,
		ErasedAt:       &nlLeftAt,
		CreatedAt:      nlJoinedAt,
		UpdatedAt:      nlLeftAt,
	}

	tests := []struct {
		name string
		sub  nldomain.Subscriber
		want string
	}{
		{name: "active member", sub: activeSubscriber(), want: `{` + activeSubscriberJSON + `}`},
		{
			name: "erased guest",
			sub:  erased,
			want: `{"id": "0198a1b2-0000-7000-8000-0000000e0001", "email": "", "displayName": "", "userId": null,
				"status": "erased", "format": "plaintext", "source": "public", "memberships": [],
				"optedInAt": null, "unsubscribedAt": "2026-08-01T10:00:00Z", "bouncedAt": "2026-08-01T10:00:00Z",
				"complainedAt": "2026-08-01T10:00:00Z", "erasedAt": "2026-08-03T10:00:00Z",
				"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-03T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterSubscriber(tt.sub))
		})
	}
}

func TestNewsletterSubscriberDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		history []nldomain.ConsentEvent
		want    string
	}{
		{name: "no history", history: nil, want: `{` + activeSubscriberJSON + `, "consentHistory": []}`},
		{
			name: "events in order",
			history: []nldomain.ConsentEvent{
				{Event: nldomain.ConsentListLeft, ListSlug: "launches", Source: "preferences", ReasonCode: "too_many",
					Feedback: "fewer please", IPHash: "hidden", OccurredAt: nlLeftAt},
				{Event: nldomain.ConsentConfirmed, Source: "public", OccurredAt: nlJoinedAt},
			},
			want: `{` + activeSubscriberJSON + `, "consentHistory": [
				{"event": "list_left", "list": "launches", "source": "preferences", "reasonCode": "too_many",
				 "feedback": "fewer please", "occurredAt": "2026-08-03T10:00:00Z"},
				{"event": "confirmed", "list": "", "source": "public", "reasonCode": "", "feedback": "",
				 "occurredAt": "2026-08-01T10:00:00Z"}
			]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterSubscriberDetail(activeSubscriber(), tt.history))
		})
	}
}

func TestNewsletterIssue(t *testing.T) {
	t.Parallel()

	sent := nldomain.Issue{
		UUID:         nlIssueID,
		Subject:      "August",
		Preheader:    "What's new",
		BodyMarkdown: "# Hello",
		Lists:        []string{"weekly"},
		Status:       nldomain.IssueSent,
		SendAt:       &nlJoinedAt,
		QueuedAt:     &nlJoinedAt,
		StartedAt:    &nlJoinedAt,
		CompletedAt:  &nlLeftAt,
		SentCount:    120,
		FailedCount:  2,
		CreatedBy:    &nlStaffID,
		UpdatedBy:    &nlStaffID,
		CreatedAt:    nlJoinedAt,
		UpdatedAt:    nlLeftAt,
	}

	const sentJSON = `"id": "0198a1b2-0000-7000-8000-0000000e0003", "subject": "August", "preheader": "What's new",
		"lists": ["weekly"], "status": "sent", "sendAt": "2026-08-01T10:00:00Z", "queuedAt": "2026-08-01T10:00:00Z",
		"startedAt": "2026-08-01T10:00:00Z", "completedAt": "2026-08-03T10:00:00Z", "sentCount": 120, "failedCount": 2,
		"createdBy": "0198a1b2-0000-7000-8000-0000000e0004", "updatedBy": "0198a1b2-0000-7000-8000-0000000e0004",
		"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-03T10:00:00Z"`

	tests := []struct {
		name     string
		issue    nldomain.Issue
		withBody bool
		want     string
	}{
		{name: "list view omits the body", issue: sent, withBody: false, want: `{` + sentJSON + `}`},
		{name: "detail view includes the body", issue: sent, withBody: true, want: `{` + sentJSON + `, "bodyMarkdown": "# Hello"}`},
		{
			name:  "draft",
			issue: nldomain.Issue{UUID: nlIssueID, Status: nldomain.IssueDraft, CreatedAt: nlJoinedAt, UpdatedAt: nlJoinedAt},
			want: `{"id": "0198a1b2-0000-7000-8000-0000000e0003", "subject": "", "preheader": "", "lists": null,
				"status": "draft", "sendAt": null, "queuedAt": null, "startedAt": null, "completedAt": null,
				"sentCount": 0, "failedCount": 0, "createdBy": null, "updatedBy": null,
				"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-01T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, NewsletterIssue(tt.issue, tt.withBody))
		})
	}
}

func TestNewsletterProviderConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      nldomain.Config
		lists    []nldomain.List
		provider NewsletterProvider
		want     string
	}{
		{
			name:     "missing postal address is not ready",
			cfg:      nldomain.Config{FromName: "Blog", FromEmail: "news@example.com", ConfirmTTL: 48 * time.Hour, PostalAddress: "  "},
			provider: NewsletterProvider{Name: "log"},
			want: `{"fromName": "Blog", "fromEmail": "news@example.com", "replyTo": "", "postalAddress": "  ",
				"confirmTtlHours": 48, "doubleOptinRequired": false, "readyToSend": false,
				"updatedAt": null, "updatedBy": null, "lists": [],
				"provider": {"name": "log", "secretConfigured": false, "sendingEnabled": false, "webhookEnabled": false}}`,
		},
		{
			name: "complete config",
			cfg: nldomain.Config{
				FromName: "Blog", FromEmail: "news@example.com", ReplyTo: "hi@example.com", PostalAddress: "1 Main St",
				ConfirmTTL: 90 * time.Minute, DoubleOptInRequired: true, UpdatedAt: &nlLeftAt, UpdatedBy: &nlStaffID,
			},
			lists: nlLists()[:1],
			provider: NewsletterProvider{
				Name: "postmark", Endpoint: "https://api.postmark.example", SecretConfigured: true, SendingEnabled: true, WebhookEnabled: true,
			},
			want: `{"fromName": "Blog", "fromEmail": "news@example.com", "replyTo": "hi@example.com",
				"postalAddress": "1 Main St", "confirmTtlHours": 1, "doubleOptinRequired": true, "readyToSend": true,
				"updatedAt": "2026-08-03T10:00:00Z", "updatedBy": "0198a1b2-0000-7000-8000-0000000e0004",
				"lists": [{"slug": "weekly", "name": "Weekly", "description": "Digest", "isDefault": true, "position": 1, "archivedAt": null}],
				"provider": {"name": "postmark", "endpoint": "https://api.postmark.example", "secretConfigured": true,
					"sendingEnabled": true, "webhookEnabled": true}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := NewsletterProviderConfig(tt.cfg, tt.lists, tt.provider)
			assert.Equal(t, tt.provider, out["provider"])
			assertJSON(t, tt.want, out)
		})
	}
}
