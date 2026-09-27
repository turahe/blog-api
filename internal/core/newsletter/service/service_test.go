package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		deps Deps
		cfg  Config
		want Config
	}{
		{
			name: "zero config takes defaults",
			want: Config{
				BatchSize: DefaultBatchSize, MaxAttempts: DefaultMaxAttempts,
				ClaimTimeout: DefaultClaimTimeout, LinkTTL: DefaultLinkTTL,
			},
		},
		{
			name: "explicit config is kept",
			deps: Deps{Sender: &captureSender{}},
			cfg:  Config{BatchSize: 5, MaxAttempts: 2, ClaimTimeout: time.Minute, LinkTTL: time.Hour},
			want: Config{BatchSize: 5, MaxAttempts: 2, ClaimTimeout: time.Minute, LinkTTL: time.Hour},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := New(tt.deps, tt.cfg)
			require.Equal(t, tt.want, svc.cfg)
			require.NotNil(t, svc.Logger)
			require.Equal(t, tt.deps.Sender != nil, svc.SendingEnabled())
		})
	}
}

func pendingSubscriber(f *fixture, email string, lists ...string) domain.Subscriber {
	now := f.clock.Now()
	sub := domain.Subscriber{
		UUID: uuid.New(), Email: email, Status: domain.StatusPending, Format: domain.FormatHTML,
		Source: domain.SourcePublic, CreatedAt: now, UpdatedAt: now,
	}

	for _, slug := range lists {
		sub.SetMembership(slug, domain.MembershipPending, now)
	}

	f.repo.putSubscriber(sub)

	return sub
}

func TestSubscribe(t *testing.T) {
	t.Parallel()

	base := SubscribeInput{Email: " Reader@Example.TEST ", Name: " Reader ", IP: "203.0.113.9", UserAgent: "agent"}

	tests := []struct {
		name    string
		edit    func(*Deps, *Config)
		setup   func(f *fixture)
		in      func(in *SubscribeInput)
		wantErr error
		check   func(t *testing.T, f *fixture)
	}{
		{
			name: "new address gets a confirmation link",
			check: func(t *testing.T, f *fixture) {
				sub := f.repo.onlySubscriber(t)
				assert.Equal(t, "reader@example.test", sub.Email)
				assert.Equal(t, "Reader", sub.DisplayName)
				assert.Equal(t, domain.StatusPending, sub.Status)
				assert.Equal(t, domain.SourcePublic, sub.Source)
				assert.Equal(t, "agent", sub.UserAgent)
				assert.Equal(t, hashIdentityHex("203.0.113.9"), sub.IPHash)
				assert.Equal(t, 1, sub.ConfirmSends)

				m, ok := sub.Membership("weekly")
				require.True(t, ok)
				assert.Equal(t, domain.MembershipPending, m.State)

				confirms := f.mailer.sentConfirms()
				require.Len(t, confirms, 1)
				assert.Equal(t, "reader@example.test", confirms[0].To)
				assert.Equal(t, "Reader", confirms[0].Name)
				assert.Equal(t, []string{"Weekly digest"}, confirms[0].Lists)
				assert.Equal(t, testNow.Add(domain.DefaultConfirmTTL), confirms[0].ExpiresAt)

				tokens := f.repo.tokensFor(sub.UUID, domain.PurposeConfirm)
				require.Len(t, tokens, 1)
				assert.Equal(t, hashToken(confirms[0].Token), tokens[0].Hash)
				assert.Equal(t, []string{domain.ConsentConfirmSent}, f.repo.consentKinds(sub.UUID))
			},
		},
		{
			name: "named lists are deduplicated",
			in:   func(in *SubscribeInput) { in.Lists = []string{"product", "product", "weekly"} },
			check: func(t *testing.T, f *fixture) {
				assert.Equal(t, []string{"Product news", "Weekly digest"}, f.mailer.sentConfirms()[0].Lists)
			},
		},
		{
			name: "honeypot is silently accepted",
			in:   func(in *SubscribeInput) { in.Honeypot = "bot" },
			check: func(t *testing.T, f *fixture) {
				assert.Zero(t, f.repo.subscriberCount())
				assert.Empty(t, f.mailer.sentConfirms())
			},
		},
		{name: "invalid email", in: func(in *SubscribeInput) { in.Email = "nope" }, wantErr: domain.ErrValidation},
		{name: "name with markup", in: func(in *SubscribeInput) { in.Name = "<b>x</b>" }, wantErr: domain.ErrValidation},
		{name: "name too long", in: func(in *SubscribeInput) { in.Name = strings.Repeat("a", 101) }, wantErr: domain.ErrValidation},
		{name: "unknown format", in: func(in *SubscribeInput) { in.Format = "pdf" }, wantErr: domain.ErrValidation},
		{
			name:    "captcha rejected",
			edit:    func(d *Deps, _ *Config) { d.Captcha = fakeCaptcha{ok: false} },
			wantErr: domain.ErrCaptcha,
		},
		{
			name:    "captcha unavailable",
			edit:    func(d *Deps, _ *Config) { d.Captcha = fakeCaptcha{err: errBoom} },
			wantErr: errBoom,
		},
		{
			name: "captcha passes",
			edit: func(d *Deps, _ *Config) { d.Captcha = fakeCaptcha{ok: true} },
			check: func(t *testing.T, f *fixture) {
				assert.Len(t, f.mailer.sentConfirms(), 1)
			},
		},
		{name: "unknown list", in: func(in *SubscribeInput) { in.Lists = []string{"nope"} }, wantErr: domain.ErrValidation},
		{name: "archived list", in: func(in *SubscribeInput) { in.Lists = []string{"old"} }, wantErr: domain.ErrValidation},
		{
			name:    "too many lists",
			in:      func(in *SubscribeInput) { in.Lists = make([]string, domain.MaxLists+1) },
			wantErr: domain.ErrValidation,
		},
		{
			name:    "no default list",
			setup:   func(f *fixture) { f.repo.lists = f.repo.lists[1:] },
			wantErr: domain.ErrNotConfigured,
		},
		{name: "lists unavailable", setup: func(f *fixture) { f.repo.failOn("Lists", errBoom) }, wantErr: errBoom},
		{name: "config unavailable", setup: func(f *fixture) { f.repo.failOn("Config", errBoom) }, wantErr: errBoom},
		{
			name:    "lookup fails",
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByEmail", errBoom) },
			wantErr: errBoom,
		},
		{
			name: "suppressed address is silently ignored",
			setup: func(f *fixture) {
				sub := f.activeSubscriber("reader@example.test")
				sub.Status = domain.StatusBounced
				f.repo.putSubscriber(sub)
			},
			check: func(t *testing.T, f *fixture) {
				assert.Zero(t, f.repo.callCount("UpdateSubscriber"))
				assert.Empty(t, f.mailer.sentConfirms())
			},
		},
		{
			name:  "active member gets no email",
			setup: func(f *fixture) { f.activeSubscriber("reader@example.test", "weekly") },
			check: func(t *testing.T, f *fixture) {
				sub := f.repo.onlySubscriber(t)
				assert.Equal(t, domain.StatusActive, sub.Status)
				assert.Empty(t, f.repo.tokensFor(sub.UUID, domain.PurposeConfirm))
				assert.Empty(t, f.mailer.sentConfirms())
			},
		},
		{
			name: "pending address gets a fresh link and the old one is revoked",
			setup: func(f *fixture) {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				f.token(sub.UUID, domain.PurposeConfirm)
			},
			check: func(t *testing.T, f *fixture) {
				sub := f.repo.onlySubscriber(t)
				tokens := f.repo.tokensFor(sub.UUID, domain.PurposeConfirm)
				require.Len(t, tokens, 1)
				assert.Equal(t, hashToken(f.mailer.lastConfirmToken(t)), tokens[0].Hash)
			},
		},
		{
			name:  "concurrent create is answered the same way",
			setup: func(f *fixture) { f.repo.failOn("CreateSubscriber", domain.ErrConflict) },
			check: func(t *testing.T, f *fixture) {
				assert.Empty(t, f.mailer.sentConfirms())
			},
		},
		{name: "create fails", setup: func(f *fixture) { f.repo.failOn("CreateSubscriber", errBoom) }, wantErr: errBoom},
		{
			name: "update fails",
			setup: func(f *fixture) {
				pendingSubscriber(f, "reader@example.test", "weekly")
				f.repo.failOn("UpdateSubscriber", errBoom)
			},
			wantErr: errBoom,
		},
		{name: "revoke fails", setup: func(f *fixture) { f.repo.failOn("RevokeTokens", errBoom) }, wantErr: errBoom},
		{name: "token store fails", setup: func(f *fixture) { f.repo.failOn("CreateToken", errBoom) }, wantErr: errBoom},
		{name: "consent fails", setup: func(f *fixture) { f.repo.failOn("AppendConsent", errBoom) }, wantErr: errBoom},
		{
			name:  "mailer failure is logged, not returned",
			setup: func(f *fixture) { f.mailer.err = errBoom },
			check: func(t *testing.T, f *fixture) {
				assert.Contains(t, f.logs.String(), "confirmation email failed")
			},
		},
		{
			name: "without a mailer the token is still issued",
			edit: func(d *Deps, _ *Config) { d.Mailer = nil },
			check: func(t *testing.T, f *fixture) {
				sub := f.repo.onlySubscriber(t)
				assert.Len(t, f.repo.tokensFor(sub.UUID, domain.PurposeConfirm), 1)
				assert.Empty(t, f.mailer.sentConfirms())
			},
		},
		{
			name:  "list names fall back to slugs",
			setup: func(f *fixture) { f.repo.failOn("Lists(archived)", errBoom) },
			check: func(t *testing.T, f *fixture) {
				assert.Equal(t, []string{"weekly"}, f.mailer.sentConfirms()[0].Lists)
			},
		},
		{
			name: "identity hasher keys the IP hash",
			edit: func(d *Deps, _ *Config) { d.IdentityHasher = prefixHasher{} },
			check: func(t *testing.T, f *fixture) {
				sub := f.repo.onlySubscriber(t)
				assert.Equal(t, "mac:203.0.113.9", sub.IPHash)
				assert.Equal(t, "mac:203.0.113.9", f.repo.consentEvents()[0].IPHash)
			},
		},
		{
			name: "long user agent is truncated",
			in:   func(in *SubscribeInput) { in.UserAgent = strings.Repeat("u", 600) },
			check: func(t *testing.T, f *fixture) {
				assert.Len(t, f.repo.onlySubscriber(t).UserAgent, 512)
			},
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

			in := base
			if tt.in != nil {
				tt.in(&in)
			}

			err := f.svc.Subscribe(t.Context(), in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

func TestSubscribeThrottlesConfirmations(t *testing.T) {
	t.Parallel()

	f := newFixture()
	in := SubscribeInput{Email: "reader@example.test"}

	for range confirmPerWindow + 1 {
		require.NoError(t, f.svc.Subscribe(t.Context(), in))
	}

	require.Len(t, f.mailer.sentConfirms(), confirmPerWindow, "the fourth request inside the window sends nothing")

	f.clock.Advance(confirmWindow)
	require.NoError(t, f.svc.Subscribe(t.Context(), in))
	require.Len(t, f.mailer.sentConfirms(), confirmPerWindow+1, "a new window allows another email")
	require.Equal(t, 1, f.repo.onlySubscriber(t).ConfirmSends)
}

func TestConfirm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		edit    func(*Deps, *Config)
		setup   func(t *testing.T, f *fixture) string
		wantErr error
		check   func(t *testing.T, f *fixture, sub domain.Subscriber)
	}{
		{
			name: "activates pending lists and sends a welcome",
			setup: func(t *testing.T, f *fixture) string {
				require.NoError(t, f.svc.Subscribe(t.Context(), SubscribeInput{Email: "reader@example.test", Name: "Reader"}))
				return f.mailer.lastConfirmToken(t)
			},
			check: func(t *testing.T, f *fixture, got domain.Subscriber) {
				sub := f.repo.subscriber(t, got.UUID)
				assert.Equal(t, domain.StatusActive, got.Status)
				assert.Equal(t, domain.StatusActive, sub.Status)
				assert.Equal(t, testNow, *sub.OptedInAt)
				assert.Equal(t, []string{"weekly"}, sub.ActiveLists())
				assert.Equal(t,
					[]string{domain.ConsentConfirmSent, domain.ConsentConfirmed, domain.ConsentListJoined},
					f.repo.consentKinds(sub.UUID))
				assert.Equal(t, []string{"confirmed"}, f.changes())
				assert.NotNil(t, f.repo.tokensFor(sub.UUID, domain.PurposeConfirm)[0].UsedAt)

				welcomes := f.mailer.sentWelcomes()
				require.Len(t, welcomes, 1)
				assert.Equal(t, "Reader", welcomes[0].Name)
				assert.Equal(t, []string{"Weekly digest"}, welcomes[0].Lists)

				prefs := f.repo.tokensFor(sub.UUID, domain.PurposePreferences)
				require.Len(t, prefs, 1)
				assert.Equal(t, hashToken(welcomes[0].PreferencesToken), prefs[0].Hash)
				assert.Equal(t, testNow.Add(DefaultLinkTTL), prefs[0].ExpiresAt)
			},
		},
		{
			name: "active subscriber keeps its opt-in time when joining another list",
			setup: func(_ *testing.T, f *fixture) string {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				sub.OptedInAt = new(testNow.Add(-time.Hour))
				sub.SetMembership("product", domain.MembershipPending, testNow)
				f.repo.putSubscriber(sub)

				return f.token(sub.UUID, domain.PurposeConfirm)
			},
			check: func(t *testing.T, f *fixture, got domain.Subscriber) {
				assert.Equal(t, testNow.Add(-time.Hour), *got.OptedInAt)
				assert.Equal(t, []string{"weekly", "product"}, got.ActiveLists())
			},
		},
		{
			name: "used token",
			setup: func(_ *testing.T, f *fixture) string {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				return f.repo.putToken("used", domain.Token{
					SubscriberID: sub.UUID, Purpose: domain.PurposeConfirm, ExpiresAt: testNow.Add(time.Hour), UsedAt: &testNow,
				})
			},
			wantErr: domain.ErrTokenUsed,
		},
		{
			name: "token used concurrently",
			setup: func(_ *testing.T, f *fixture) string {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				f.repo.hook("UseToken", func(r *memRepo) {
					for i := range r.tokens {
						r.tokens[i].UsedAt = &testNow
					}
				})

				return f.token(sub.UUID, domain.PurposeConfirm)
			},
			wantErr: domain.ErrTokenUsed,
		},
		{
			name: "unknown token",
			setup: func(*testing.T, *fixture) string {
				return "missing"
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "expired token",
			setup: func(_ *testing.T, f *fixture) string {
				raw := f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
				f.clock.Advance(time.Hour)

				return raw
			},
			wantErr: domain.ErrTokenExpired,
		},
		{
			name: "use fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.repo.failOn("UseToken", errBoom)
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "subscriber lookup fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.repo.failOn("Subscriber", errBoom)
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "erased subscriber",
			setup: func(_ *testing.T, f *fixture) string {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				sub.Status = domain.StatusErased
				f.repo.putSubscriber(sub)

				return f.token(sub.UUID, domain.PurposeConfirm)
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "update fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.repo.failOn("UpdateSubscriber", errBoom)
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "consent fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.repo.failOn("AppendConsent", errBoom)
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "event fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.events.Err = errBoom
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "preferences token fails",
			setup: func(_ *testing.T, f *fixture) string {
				f.repo.failOn("CreateToken", errBoom)
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			wantErr: errBoom,
		},
		{
			name: "welcome failure is logged, not returned",
			setup: func(_ *testing.T, f *fixture) string {
				f.mailer.err = errBoom
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			check: func(t *testing.T, f *fixture, got domain.Subscriber) {
				assert.Equal(t, domain.StatusActive, got.Status)
				assert.Contains(t, f.logs.String(), "welcome email failed")
			},
		},
		{
			name: "without a mailer no welcome is sent",
			edit: func(d *Deps, _ *Config) { d.Mailer = nil },
			setup: func(_ *testing.T, f *fixture) string {
				return f.token(pendingSubscriber(f, "reader@example.test", "weekly").UUID, domain.PurposeConfirm)
			},
			check: func(t *testing.T, f *fixture, got domain.Subscriber) {
				assert.Equal(t, domain.StatusActive, got.Status)
				assert.Empty(t, f.mailer.sentWelcomes())
			},
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
			raw := tt.setup(t, f)

			sub, err := f.svc.Confirm(t.Context(), raw, "203.0.113.9")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			if tt.check != nil {
				tt.check(t, f, sub)
			}
		})
	}
}

func TestResendConfirm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		email     string
		setup     func(f *fixture)
		wantErr   error
		wantLists []string
	}{
		{
			name:  "pending lists get a new link",
			email: "Reader@Example.test",
			setup: func(f *fixture) {
				sub := pendingSubscriber(f, "reader@example.test", "weekly", "old", "ghost")
				f.token(sub.UUID, domain.PurposeConfirm)
			},
			wantLists: []string{"Weekly digest", "Old list", "ghost"},
		},
		{name: "invalid email", email: "nope", wantErr: domain.ErrValidation},
		{
			name: "config unavailable", email: "reader@example.test",
			setup:   func(f *fixture) { f.repo.failOn("Config", errBoom) },
			wantErr: errBoom,
		},
		{name: "unknown address", email: "reader@example.test"},
		{
			name: "lookup fails", email: "reader@example.test",
			setup:   func(f *fixture) { f.repo.failOn("SubscriberByEmail", errBoom) },
			wantErr: errBoom,
		},
		{
			name: "suppressed address", email: "reader@example.test",
			setup: func(f *fixture) {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				sub.Status = domain.StatusComplained
				f.repo.putSubscriber(sub)
			},
		},
		{
			name: "nothing pending", email: "reader@example.test",
			setup: func(f *fixture) { f.activeSubscriber("reader@example.test", "weekly") },
		},
		{
			name: "limit reached", email: "reader@example.test",
			setup: func(f *fixture) {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				sub.ConfirmSends, sub.ConfirmWindowStart = confirmPerWindow, &testNow
				f.repo.putSubscriber(sub)
			},
		},
		{
			name: "update fails", email: "reader@example.test",
			setup: func(f *fixture) {
				pendingSubscriber(f, "reader@example.test", "weekly")
				f.repo.failOn("UpdateSubscriber", errBoom)
			},
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

			err := f.svc.ResendConfirm(t.Context(), tt.email, "203.0.113.9")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			confirms := f.mailer.sentConfirms()
			if tt.wantLists == nil {
				require.Empty(t, confirms)
				return
			}

			require.Len(t, confirms, 1)
			require.Equal(t, tt.wantLists, confirms[0].Lists)

			sub := f.repo.onlySubscriber(t)
			tokens := f.repo.tokensFor(sub.UUID, domain.PurposeConfirm)
			require.Len(t, tokens, 1, "the earlier link is revoked")
			require.Equal(t, hashToken(confirms[0].Token), tokens[0].Hash)
		})
	}
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *fixture) (domain.Subscriber, string)
		in      UnsubscribeInput
		wantErr error
		check   func(t *testing.T, f *fixture, sub domain.Subscriber)
	}{
		{
			name: "leaves every list with feedback",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly", "product")
				f.token(sub.UUID, domain.PurposeConfirm)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			in: UnsubscribeInput{ReasonCode: "too_frequent", Feedback: "daily is a lot", IP: "203.0.113.9"},
			check: func(t *testing.T, f *fixture, sub domain.Subscriber) {
				got := f.repo.subscriber(t, sub.UUID)
				assert.Equal(t, domain.StatusUnsubscribed, got.Status)
				assert.Equal(t, testNow, *got.UnsubscribedAt)
				assert.Empty(t, got.ActiveLists())
				assert.Empty(t, f.repo.tokensFor(sub.UUID, domain.PurposeConfirm), "pending confirmations are revoked")
				assert.Equal(t, []string{"unsubscribed"}, f.changes())

				events := f.repo.consentEvents()
				require.Len(t, events, 1)
				assert.Equal(t, domain.ConsentUnsubscribed, events[0].Event)
				assert.Equal(t, domain.ConsentSourceToken, events[0].Source)
				assert.Equal(t, "too_frequent", events[0].ReasonCode)
				assert.Equal(t, "daily is a lot", events[0].Feedback)
				assert.Equal(t, hashIdentityHex("203.0.113.9"), events[0].IPHash)
			},
		},
		{
			name: "pending subscriber is unsubscribed",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := pendingSubscriber(f, "reader@example.test", "weekly")
				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			check: func(t *testing.T, f *fixture, sub domain.Subscriber) {
				assert.Equal(t, domain.StatusUnsubscribed, f.repo.subscriber(t, sub.UUID).Status)
			},
		},
		{
			name: "repeating it is harmless",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				sub.Status = domain.StatusUnsubscribed
				sub.SetMembership("weekly", domain.MembershipLeft, testNow)
				f.repo.putSubscriber(sub)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			check: func(t *testing.T, f *fixture, _ domain.Subscriber) {
				assert.Zero(t, f.repo.callCount("UpdateSubscriber"))
				assert.Empty(t, f.repo.consentEvents())
				assert.Empty(t, f.changes())
			},
		},
		{
			name: "late feedback is still recorded",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test")
				sub.Status = domain.StatusUnsubscribed
				f.repo.putSubscriber(sub)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			in: UnsubscribeInput{ReasonCode: "not_relevant"},
			check: func(t *testing.T, f *fixture, sub domain.Subscriber) {
				assert.Equal(t, []string{domain.ConsentUnsubscribed}, f.repo.consentKinds(sub.UUID))
				assert.Equal(t, domain.StatusUnsubscribed, f.repo.subscriber(t, sub.UUID).Status)
			},
		},
		{
			name: "suppressed address keeps its status",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				sub.Status = domain.StatusBounced
				f.repo.putSubscriber(sub)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			check: func(t *testing.T, f *fixture, sub domain.Subscriber) {
				got := f.repo.subscriber(t, sub.UUID)
				assert.Equal(t, domain.StatusBounced, got.Status)
				assert.Empty(t, got.ActiveLists())
			},
		},
		{
			name: "invalid feedback",
			setup: func(*fixture) (domain.Subscriber, string) {
				return domain.Subscriber{}, "unused"
			},
			in:      UnsubscribeInput{ReasonCode: "spam"},
			wantErr: domain.ErrValidation,
		},
		{
			name: "confirm token cannot unsubscribe",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				return sub, f.token(sub.UUID, domain.PurposeConfirm)
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "subscriber lookup fails",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				f.repo.failOn("Subscriber", errBoom)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			wantErr: errBoom,
		},
		{
			name: "erased subscriber",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				sub.Status = domain.StatusErased
				f.repo.putSubscriber(sub)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "update fails",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				f.repo.failOn("UpdateSubscriber", errBoom)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			wantErr: errBoom,
		},
		{
			name: "revoke fails",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				f.repo.failOn("RevokeTokens", errBoom)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			wantErr: errBoom,
		},
		{
			name: "consent fails",
			setup: func(f *fixture) (domain.Subscriber, string) {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				f.repo.failOn("AppendConsent", errBoom)

				return sub, f.token(sub.UUID, domain.PurposeUnsubscribe)
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			sub, raw := tt.setup(f)
			tt.in.Token = raw

			err := f.svc.Unsubscribe(t.Context(), tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			tt.check(t, f, sub)
		})
	}
}

func TestPreferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *fixture) string
		wantErr error
	}{
		{
			name: "returns the subscriber and the open lists",
			setup: func(f *fixture) string {
				return f.token(f.activeSubscriber("reader@example.test", "weekly").UUID, domain.PurposePreferences)
			},
		},
		{
			name: "unsubscribe token is refused",
			setup: func(f *fixture) string {
				return f.token(f.activeSubscriber("reader@example.test", "weekly").UUID, domain.PurposeUnsubscribe)
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "subscriber lookup fails",
			setup: func(f *fixture) string {
				f.repo.failOn("Subscriber", errBoom)
				return f.token(f.activeSubscriber("reader@example.test").UUID, domain.PurposePreferences)
			},
			wantErr: errBoom,
		},
		{
			name: "erased subscriber",
			setup: func(f *fixture) string {
				sub := f.activeSubscriber("reader@example.test")
				sub.Status = domain.StatusErased
				f.repo.putSubscriber(sub)

				return f.token(sub.UUID, domain.PurposePreferences)
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "lists unavailable",
			setup: func(f *fixture) string {
				f.repo.failOn("Lists", errBoom)
				return f.token(f.activeSubscriber("reader@example.test").UUID, domain.PurposePreferences)
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			raw := tt.setup(f)

			got, err := f.svc.Preferences(t.Context(), raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "reader@example.test", got.Subscriber.Email)
			require.Len(t, got.Lists, 2, "archived lists are hidden")
		})
	}
}

func TestUpdatePreferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setup        func(f *fixture) domain.Subscriber
		in           PreferencesInput
		wantErr      error
		wantStatus   domain.Status
		wantActive   []string
		wantConsent  []string
		wantChanges  []string
		wantFormat   domain.Format
		wantNoUpdate bool
		badToken     bool
	}{
		{
			name:     "unknown token",
			setup:    func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			badToken: true,
			wantErr:  domain.ErrTokenInvalid,
		},
		{
			name:        "format change",
			setup:       func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:          PreferencesInput{Format: new("plaintext")},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentFormatChanged},
			wantChanges: []string{"preferences_updated"},
			wantFormat:  domain.FormatPlaintext,
		},
		{
			name:         "same format changes nothing",
			setup:        func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:           PreferencesInput{Format: new("html")},
			wantStatus:   domain.StatusActive,
			wantActive:   []string{"weekly"},
			wantFormat:   domain.FormatHTML,
			wantNoUpdate: true,
		},
		{
			name:        "switch lists",
			setup:       func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:          PreferencesInput{Lists: &[]string{"product"}},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"product"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentListLeft},
			wantChanges: []string{"preferences_updated"},
			wantFormat:  domain.FormatHTML,
		},
		{
			name: "a pending list outside the set is dropped without a left event",
			setup: func(f *fixture) domain.Subscriber {
				sub := f.activeSubscriber("reader@example.test", "weekly")
				sub.SetMembership("product", domain.MembershipPending, testNow)
				f.repo.putSubscriber(sub)

				return sub
			},
			in:          PreferencesInput{Format: new("plaintext"), Lists: &[]string{"weekly"}},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentFormatChanged},
			wantChanges: []string{"preferences_updated"},
			wantFormat:  domain.FormatPlaintext,
		},
		{
			name: "choosing lists resubscribes",
			setup: func(f *fixture) domain.Subscriber {
				sub := f.activeSubscriber("reader@example.test")
				sub.Status, sub.UnsubscribedAt = domain.StatusUnsubscribed, &testNow
				f.repo.putSubscriber(sub)

				return sub
			},
			in:          PreferencesInput{Lists: &[]string{"weekly"}},
			wantStatus:  domain.StatusActive,
			wantActive:  []string{"weekly"},
			wantConsent: []string{domain.ConsentListJoined, domain.ConsentResubscribed},
			wantChanges: []string{"resubscribed"},
			wantFormat:  domain.FormatHTML,
		},
		{
			name:        "an empty set unsubscribes",
			setup:       func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:          PreferencesInput{Lists: &[]string{}},
			wantStatus:  domain.StatusUnsubscribed,
			wantConsent: []string{domain.ConsentListLeft, domain.ConsentUnsubscribed},
			wantChanges: []string{"unsubscribed"},
			wantFormat:  domain.FormatHTML,
		},
		{
			name:        "unsubscribe all",
			setup:       func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:          PreferencesInput{UnsubscribeAll: true, Format: new("plaintext")},
			wantStatus:  domain.StatusUnsubscribed,
			wantConsent: []string{domain.ConsentUnsubscribed},
			wantChanges: []string{"unsubscribed"},
			wantFormat:  domain.FormatHTML,
		},
		{
			name:    "invalid format",
			setup:   func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:      PreferencesInput{Format: new("pdf")},
			wantErr: domain.ErrValidation,
		},
		{
			name:    "unknown list",
			setup:   func(f *fixture) domain.Subscriber { return f.activeSubscriber("reader@example.test", "weekly") },
			in:      PreferencesInput{Lists: &[]string{"nope"}},
			wantErr: domain.ErrValidation,
		},
		{
			name: "lists unavailable",
			setup: func(f *fixture) domain.Subscriber {
				f.repo.failOn("Lists", errBoom)
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			in:      PreferencesInput{Lists: &[]string{"weekly"}},
			wantErr: errBoom,
		},
		{
			name: "subscriber lookup fails",
			setup: func(f *fixture) domain.Subscriber {
				f.repo.failOn("Subscriber", errBoom)
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			wantErr: errBoom,
		},
		{
			name: "erased subscriber",
			setup: func(f *fixture) domain.Subscriber {
				sub := f.activeSubscriber("reader@example.test")
				sub.Status = domain.StatusErased
				f.repo.putSubscriber(sub)

				return sub
			},
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "update fails",
			setup: func(f *fixture) domain.Subscriber {
				f.repo.failOn("UpdateSubscriber", errBoom)
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			in:      PreferencesInput{Format: new("plaintext")},
			wantErr: errBoom,
		},
		{
			name: "consent fails",
			setup: func(f *fixture) domain.Subscriber {
				f.repo.failOn("AppendConsent", errBoom)
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			in:      PreferencesInput{Format: new("plaintext")},
			wantErr: errBoom,
		},
		{
			name: "event fails",
			setup: func(f *fixture) domain.Subscriber {
				f.events.Err = errBoom
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			in:      PreferencesInput{Format: new("plaintext")},
			wantErr: errBoom,
		},
		{
			name: "reading back fails",
			setup: func(f *fixture) domain.Subscriber {
				f.repo.failOn("Lists", errBoom)
				return f.activeSubscriber("reader@example.test", "weekly")
			},
			in:      PreferencesInput{Format: new("plaintext")},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			sub := tt.setup(f)

			raw := f.token(sub.UUID, domain.PurposePreferences)
			if tt.badToken {
				raw = "unknown"
			}

			got, err := f.svc.UpdatePreferences(t.Context(), raw, tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Subscriber.Status)
			assert.Equal(t, tt.wantActive, got.Subscriber.ActiveLists())
			assert.Equal(t, tt.wantFormat, got.Subscriber.Format)
			assert.Equal(t, tt.wantConsent, f.repo.consentKinds(sub.UUID))
			assert.Equal(t, tt.wantChanges, f.changes())
			assert.Len(t, got.Lists, 2)

			if tt.wantNoUpdate {
				assert.Zero(t, f.repo.callCount("UpdateSubscriber"))
			}
		})
	}
}

func TestUpdatePreferencesStampsListEvents(t *testing.T) {
	t.Parallel()

	f := newFixture()
	sub := f.activeSubscriber("reader@example.test", "weekly")

	_, err := f.svc.UpdatePreferences(t.Context(), f.token(sub.UUID, domain.PurposePreferences),
		PreferencesInput{Lists: &[]string{"product", "product"}})
	require.NoError(t, err)

	var got []string
	for _, e := range f.repo.consentEvents() {
		got = append(got, e.Event+":"+e.ListSlug)
	}

	require.Equal(t, []string{"list_joined:product", "list_left:weekly"}, got)
}

func TestAllowConfirm(t *testing.T) {
	t.Parallel()

	earlier := testNow.Add(-time.Minute)
	expired := testNow.Add(-confirmWindow)

	tests := []struct {
		name      string
		sub       domain.Subscriber
		want      bool
		wantSends int
		wantStart time.Time
	}{
		{name: "first email opens a window", want: true, wantSends: 1, wantStart: testNow},
		{
			name: "under the limit", sub: domain.Subscriber{ConfirmWindowStart: &earlier, ConfirmSends: 2},
			want: true, wantSends: 3, wantStart: earlier,
		},
		{
			name: "at the limit", sub: domain.Subscriber{ConfirmWindowStart: &earlier, ConfirmSends: confirmPerWindow},
			want: false, wantSends: confirmPerWindow, wantStart: earlier,
		},
		{
			name: "an elapsed window resets", sub: domain.Subscriber{ConfirmWindowStart: &expired, ConfirmSends: confirmPerWindow},
			want: true, wantSends: 1, wantStart: testNow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub := tt.sub
			require.Equal(t, tt.want, allowConfirm(&sub, testNow))
			require.Equal(t, tt.wantSends, sub.ConfirmSends)
			require.Equal(t, tt.wantStart, *sub.ConfirmWindowStart)
		})
	}
}

func TestLookupToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     func(f *fixture) string
		purpose domain.TokenPurpose
		wantErr error
	}{
		{
			name:    "valid token with surrounding space",
			raw:     func(f *fixture) string { return "  " + f.token(uuid.New(), domain.PurposePreferences) + " " },
			purpose: domain.PurposePreferences,
		},
		{name: "empty", raw: func(*fixture) string { return "   " }, wantErr: domain.ErrTokenInvalid},
		{
			name:    "too long",
			raw:     func(*fixture) string { return strings.Repeat("a", maxTokenLength+1) },
			wantErr: domain.ErrTokenInvalid,
		},
		{name: "unknown", raw: func(*fixture) string { return "unknown" }, wantErr: domain.ErrTokenInvalid},
		{
			name:    "another purpose looks unknown",
			raw:     func(f *fixture) string { return f.token(uuid.New(), domain.PurposeUnsubscribe) },
			purpose: domain.PurposePreferences,
			wantErr: domain.ErrTokenInvalid,
		},
		{
			name: "store fails",
			raw: func(f *fixture) string {
				f.repo.failOn("Token", errBoom)
				return "any"
			},
			wantErr: errBoom,
		},
		{
			name: "expires at its expiry instant",
			raw: func(f *fixture) string {
				return f.repo.putToken("edge", domain.Token{Purpose: domain.PurposeConfirm, ExpiresAt: testNow})
			},
			purpose: domain.PurposeConfirm,
			wantErr: domain.ErrTokenExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture()
			raw := tt.raw(f)

			got, err := f.svc.lookupToken(t.Context(), raw, tt.purpose)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, hashToken(strings.TrimSpace(raw)), got.Hash)
		})
	}
}

func TestNewToken(t *testing.T) {
	t.Parallel()

	raw, hash, err := newToken()
	require.NoError(t, err)
	require.Len(t, raw, 43, "32 bytes in unpadded base64url")
	require.Equal(t, hashToken(raw), hash)

	other, _, err := newToken()
	require.NoError(t, err)
	require.NotEqual(t, raw, other)
}

func TestCleanName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "trimmed", in: "  Ada  ", want: "Ada"},
		{name: "empty is allowed", in: "", want: ""},
		{name: "longest allowed", in: strings.Repeat("é", maxNameLength), want: strings.Repeat("é", maxNameLength)},
		{name: "too long", in: strings.Repeat("é", maxNameLength+1), wantErr: domain.ErrValidation},
		{name: "line break", in: "Ada\nBcc: x", wantErr: domain.ErrValidation},
		{name: "angle bracket", in: "Ada <x>", wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := cleanName(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		n     int
		want  string
	}{
		{name: "short value is kept", value: "abc", n: 5, want: "abc"},
		{name: "cut at a byte limit", value: "abcdef", n: 3, want: "abc"},
		{name: "never splits a rune", value: "aé", n: 2, want: "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, truncate(tt.value, tt.n))
		})
	}
}

// hashIdentityHex is the unkeyed identity hash.
func hashIdentityHex(value string) string {
	return New(Deps{}, Config{}).hashIdentity(value)
}
