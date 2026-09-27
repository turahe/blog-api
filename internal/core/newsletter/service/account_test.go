package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

var testUser = uuid.MustParse("7d9f5b0e-2c1a-4b8e-9f3d-1a2b3c4d5e6f")

func withAccount(account ports.Account) func(*Deps, *Config) {
	return func(d *Deps, _ *Config) {
		d.Accounts = fakeAccounts{accounts: map[uuid.UUID]ports.Account{testUser: account}}
	}
}

func linkedSubscriber(f *fixture, status domain.Status, lists ...string) domain.Subscriber {
	sub := f.activeSubscriber("reader@example.test", lists...)
	sub.UserID, sub.Status = &testUser, status
	f.repo.putSubscriber(sub)

	return sub
}

func TestMySubscription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		edit      func(*Deps, *Config)
		setup     func(f *fixture)
		wantErr   error
		wantFound bool
	}{
		{
			name:      "found by account link",
			setup:     func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly") },
			wantFound: true,
		},
		{
			name:      "found by account email",
			edit:      withAccount(ports.Account{Email: "Reader@Example.test"}),
			setup:     func(f *fixture) { f.activeSubscriber("reader@example.test", "weekly") },
			wantFound: true,
		},
		{name: "none without an accounts port"},
		{name: "none for an unknown email", edit: withAccount(ports.Account{Email: "other@example.test"})},
		{name: "none for an unusable account email", edit: withAccount(ports.Account{Email: "not-an-email"})},
		{
			name:    "account lookup fails",
			edit:    func(d *Deps, _ *Config) { d.Accounts = fakeAccounts{err: errBoom} },
			wantErr: errBoom,
		},
		{
			name:    "link lookup fails",
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByUser", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "lists unavailable",
			setup:   func(f *fixture) { f.repo.failOn("Lists", errBoom) },
			wantErr: errBoom,
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
			if tt.setup != nil {
				tt.setup(f)
			}

			got, found, err := f.svc.MySubscription(t.Context(), testUser)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantFound, found)
			require.Len(t, got.Lists, 2, "the lists are returned either way")

			if tt.wantFound {
				require.Equal(t, "reader@example.test", got.Subscriber.Email)
			}
		})
	}
}

func TestMySubscribe(t *testing.T) {
	t.Parallel()

	verified := ports.Account{Email: "Reader@Example.test", Name: "Reader", EmailVerified: true}
	singleOptIn := func(f *fixture) { f.repo.cfg.DoubleOptInRequired = false }

	tests := []struct {
		name        string
		edit        func(*Deps, *Config)
		setup       func(f *fixture)
		in          MySubscribeInput
		wantErr     error
		wantStatus  domain.Status
		wantActive  []string
		wantConsent []string
		wantChanges []string
		wantConfirm bool
		check       func(t *testing.T, f *fixture, got Preferences)
	}{
		{
			name:        "verified account joins at once",
			edit:        withAccount(verified),
			setup:       singleOptIn,
			in:          MySubscribeInput{IP: "203.0.113.9", UserAgent: "agent"},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentSubscribed},
			wantChanges: []string{"confirmed"},
			check: func(t *testing.T, f *fixture, got Preferences) {
				sub := f.repo.subscriber(t, got.Subscriber.UUID)
				assert.Equal(t, "reader@example.test", sub.Email)
				assert.Equal(t, "Reader", sub.DisplayName)
				assert.Equal(t, domain.SourceAccount, sub.Source)
				assert.Equal(t, testUser, *sub.UserID)
				assert.Equal(t, 1, f.repo.callCount("CreateSubscriber"))
			},
		},
		{
			name:        "an unusable account name is dropped",
			edit:        withAccount(ports.Account{Email: "reader@example.test", Name: "<script>", EmailVerified: true}),
			setup:       singleOptIn,
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentSubscribed},
			wantChanges: []string{"confirmed"},
			check: func(t *testing.T, _ *fixture, got Preferences) {
				assert.Empty(t, got.Subscriber.DisplayName)
			},
		},
		{
			name: "pending subscriber found by email is confirmed and linked",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				pendingSubscriber(f, "reader@example.test", "product")
			},
			in:          MySubscribeInput{Lists: []string{"weekly"}, Format: "plaintext"},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentSubscribed},
			wantChanges: []string{"confirmed"},
			check: func(t *testing.T, f *fixture, got Preferences) {
				assert.Equal(t, domain.FormatPlaintext, got.Subscriber.Format)
				assert.Equal(t, testUser, *got.Subscriber.UserID)
				assert.Zero(t, f.repo.callCount("CreateSubscriber"))
			},
		},
		{
			name: "unsubscribed account is resubscribed",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				linkedSubscriber(f, domain.StatusUnsubscribed)
			},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentResubscribed},
			wantChanges: []string{"confirmed"},
		},
		{
			name: "active account joins another list",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				linkedSubscriber(f, domain.StatusActive, "weekly")
			},
			in:          MySubscribeInput{Lists: []string{"product"}},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly", "product"},
			wantConsent: []string{domain.ConsentListJoined},
			wantChanges: []string{"lists_changed"},
		},
		{
			name: "rejoining an active list records nothing",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				linkedSubscriber(f, domain.StatusActive, "weekly")
			},
			wantStatus: domain.StatusActive,
			wantActive: []string{"weekly"},
		},
		{
			name:        "double opt-in sends a confirmation even when verified",
			edit:        withAccount(verified),
			wantStatus:  domain.StatusPending,
			wantConsent: []string{domain.ConsentConfirmSent},
			wantConfirm: true,
		},
		{
			name:        "unverified account gets a confirmation",
			edit:        withAccount(ports.Account{Email: "reader@example.test"}),
			setup:       singleOptIn,
			wantStatus:  domain.StatusPending,
			wantConsent: []string{domain.ConsentConfirmSent},
			wantConfirm: true,
			check: func(t *testing.T, f *fixture, got Preferences) {
				assert.Equal(t, domain.SourceAccount, got.Subscriber.Source)
				assert.Equal(t, string(domain.SourceAccount), f.repo.consentEvents()[0].Source)
			},
		},
		{name: "without an accounts port", wantErr: domain.ErrNotConfigured},
		{
			name:    "account lookup fails",
			edit:    func(d *Deps, _ *Config) { d.Accounts = fakeAccounts{err: errBoom} },
			wantErr: errBoom,
		},
		{
			name:    "unusable account email",
			edit:    withAccount(ports.Account{Email: "nope"}),
			wantErr: domain.ErrValidation,
		},
		{
			name:    "unknown list",
			edit:    withAccount(verified),
			in:      MySubscribeInput{Lists: []string{"nope"}},
			wantErr: domain.ErrValidation,
		},
		{
			name:    "config unavailable",
			edit:    withAccount(verified),
			setup:   func(f *fixture) { f.repo.failOn("Config", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "subscriber lookup fails",
			edit:    withAccount(verified),
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByUser", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "invalid format",
			edit:    withAccount(verified),
			in:      MySubscribeInput{Format: "pdf"},
			wantErr: domain.ErrValidation,
		},
		{
			name: "create fails",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				f.repo.failOn("CreateSubscriber", errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "update fails",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				linkedSubscriber(f, domain.StatusActive)
				f.repo.failOn("UpdateSubscriber", errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "consent fails",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				singleOptIn(f)
				f.repo.failOn("AppendConsent", errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "confirmation fails",
			edit: withAccount(verified),
			setup: func(f *fixture) {
				f.repo.failOn("CreateSubscriber", errBoom)
			},
			wantErr: errBoom,
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
			if tt.setup != nil {
				tt.setup(f)
			}

			in := tt.in
			in.UserID = testUser

			got, err := f.svc.MySubscribe(t.Context(), in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Subscriber.Status)
			assert.Equal(t, tt.wantActive, got.Subscriber.ActiveLists())
			assert.Equal(t, tt.wantConsent, f.repo.consentKinds(got.Subscriber.UUID))
			assert.Equal(t, tt.wantChanges, f.changes())
			assert.Equal(t, tt.wantConfirm, len(f.mailer.sentConfirms()) == 1)

			if tt.check != nil {
				tt.check(t, f, got)
			}
		})
	}
}

func TestMyUnsubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(f *fixture)
		lists       []string
		reason      string
		wantErr     error
		wantStatus  domain.Status
		wantActive  []string
		wantConsent []string
	}{
		{
			name:        "every list",
			setup:       func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly", "product") },
			reason:      "other",
			wantStatus:  domain.StatusUnsubscribed,
			wantConsent: []string{domain.ConsentUnsubscribed},
		},
		{
			name:        "one list keeps the others",
			setup:       func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly", "product") },
			lists:       []string{"weekly"},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"product"},
			wantConsent: []string{domain.ConsentListLeft},
		},
		{
			name:        "leaving the last list unsubscribes",
			setup:       func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly") },
			lists:       []string{"weekly"},
			wantStatus:  domain.StatusUnsubscribed,
			wantConsent: []string{domain.ConsentListLeft, domain.ConsentUnsubscribed},
		},
		{name: "invalid feedback", reason: "spam", wantErr: domain.ErrValidation},
		{name: "no subscription", wantErr: domain.ErrNotFound},
		{
			name:    "lookup fails",
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByUser", errBoom) },
			wantErr: errBoom,
		},
		{
			name:    "unknown list",
			setup:   func(f *fixture) { linkedSubscriber(f, domain.StatusActive, "weekly") },
			lists:   []string{"nope"},
			wantErr: domain.ErrValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			if tt.setup != nil {
				tt.setup(f)
			}

			got, err := f.svc.MyUnsubscribe(t.Context(), testUser, tt.lists, tt.reason, "", "203.0.113.9")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Subscriber.Status)
			assert.Equal(t, tt.wantActive, got.Subscriber.ActiveLists())
			assert.Equal(t, tt.wantConsent, f.repo.consentKinds(got.Subscriber.UUID))

			for _, e := range f.repo.consentEvents() {
				assert.Equal(t, string(domain.SourceAccount), e.Source)
			}
		})
	}
}
