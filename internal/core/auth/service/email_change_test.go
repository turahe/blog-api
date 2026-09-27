package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type capturingNotifier struct {
	token    string
	newEmail string
	changed  [][2]string
}

func (n *capturingNotifier) EmailChangeRequested(_ context.Context, _ userdomain.User, newEmail, rawToken string, _ time.Time) {
	n.newEmail, n.token = newEmail, rawToken
}

func (n *capturingNotifier) EmailChanged(_ context.Context, _ userdomain.User, oldEmail, newEmail string) {
	n.changed = append(n.changed, [2]string{oldEmail, newEmail})
}

func (n *capturingNotifier) PasswordReset(context.Context, userdomain.User, string, time.Time) {}

func (n *capturingNotifier) PasswordChanged(context.Context, userdomain.User) {}

type emailChangeFixture struct {
	svc      *authservice.AuthService
	users    *memUsers
	sessions *memSessions
	resets   *memResets
	notifier *capturingNotifier
	cache    *readcachetest.Memory
	user     userdomain.User
	clock    *movableClock
}

type movableClock struct{ t time.Time }

func (c *movableClock) Now() time.Time { return c.t }

func newEmailChangeFixture(t *testing.T) *emailChangeFixture {
	t.Helper()

	user := userdomain.User{
		UUID: uuid.New(), Email: "old@example.com", Username: "ada", FullName: "Ada",
		PasswordHash: "hash:Secret123456", Status: userdomain.StatusActive,
	}
	other := userdomain.User{UUID: uuid.New(), Email: "taken@example.com", Username: "bob", Status: userdomain.StatusActive}
	f := &emailChangeFixture{
		users: &memUsers{
			byEmail: map[string]userdomain.User{user.Email: user, other.Email: other},
			byID:    map[uuid.UUID]userdomain.User{user.UUID: user, other.UUID: other},
		},
		sessions: &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}},
		resets:   &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}},
		notifier: &capturingNotifier{},
		cache:    readcachetest.New(),
		user:     user,
		clock:    &movableClock{t: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)},
	}
	f.svc = authservice.New(f.users, f.sessions, f.resets, fakeHasher{}, fakeTokens{}, f.clock, uuidGen{}, authservice.Config{}, nil).
		WithEmailChange(f.notifier, f.cache)

	return f
}

func TestRequestEmailChangeValidates(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		email, password string
		want            error
	}{
		"invalid address":  {"not-an-email", "Secret123456", authdomain.ErrValidation},
		"display name":     {"Ada <ada@example.com>", "Secret123456", authdomain.ErrValidation},
		"no domain dot":    {"ada@localhost", "Secret123456", authdomain.ErrValidation},
		"wrong password":   {"new@example.com", "nope", authdomain.ErrCurrentPassword},
		"same address":     {"OLD@example.com", "Secret123456", authdomain.ErrValidation},
		"taken by another": {"taken@example.com", "Secret123456", authdomain.ErrEmailTaken},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newEmailChangeFixture(t)
			_, err := f.svc.RequestEmailChange(t.Context(), f.user.UUID, tc.email, tc.password)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, f.notifier.token)
		})
	}
}

func TestEmailChangeRoundTrip(t *testing.T) {
	t.Parallel()

	f := newEmailChangeFixture(t)
	ctx := t.Context()

	_, err := f.sessions.Create(ctx, authdomain.RefreshSession{UUID: uuid.New(), UserUUID: f.user.UUID, TokenHash: "h1"})
	require.NoError(t, err)

	first, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "  Stale@Example.com ", "Secret123456")
	require.NoError(t, err)
	require.Equal(t, "stale@example.com", first.NewEmail)
	require.Equal(t, f.clock.t.Add(time.Hour), first.ExpiresAt)
	staleToken := f.notifier.token

	request, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "new@example.com", "Secret123456")
	require.NoError(t, err)
	require.Equal(t, "new@example.com", f.notifier.newEmail)

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, staleToken)
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenUsed, "a newer request supersedes the old token")

	_, err = f.svc.ConfirmEmailChange(ctx, uuid.New(), f.notifier.token)
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenInvalid, "tokens are bound to their user")

	user, err := f.svc.ConfirmEmailChange(ctx, f.user.UUID, f.notifier.token)
	require.NoError(t, err)
	require.Equal(t, request.NewEmail, user.Email)
	require.NotNil(t, user.EmailVerifiedAt)
	require.NotNil(t, f.sessions.byHash["h1"].RevokedAt, "sessions are revoked")
	require.Equal(t, [][2]string{{"old@example.com", "new@example.com"}}, f.notifier.changed)
	require.Equal(t, 1, f.cache.Invalidations(readcache.Users))

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, f.notifier.token)
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenUsed)
}

func TestRequestEmailChangeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(f *emailChangeFixture)
		tokenFaults faults
		wantErr     error
	}{
		{name: "inactive account", setup: func(f *emailChangeFixture) { suspend(f.users, f.user.UUID) }, wantErr: authdomain.ErrUserInactive},
		{name: "availability lookup", setup: func(f *emailChangeFixture) { f.users.fail = faults{"FindByEmail": errBoom} }, wantErr: errBoom},
		{name: "issue token", tokenFaults: faults{"IssueResetToken": errBoom}, wantErr: errBoom},
		{name: "revoke earlier requests", setup: func(f *emailChangeFixture) { f.resets.fail = faults{"RevokePending": errBoom} }, wantErr: errBoom},
		{name: "store token", setup: func(f *emailChangeFixture) { f.resets.fail = faults{"Create": errBoom} }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newEmailChangeFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			svc := authservice.New(f.users, f.sessions, f.resets, fakeHasher{}, failingTokens{fail: tt.tokenFaults}, f.clock,
				uuidGen{}, authservice.Config{}, nil).WithEmailChange(f.notifier, f.cache)

			request, err := svc.RequestEmailChange(t.Context(), f.user.UUID, "new@example.com", "Secret123456")
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, authdomain.EmailChangeRequest{}, request)
			assert.Empty(t, f.notifier.token, "nothing is sent")
		})
	}
}

func suspend(users *memUsers, id uuid.UUID) {
	u := users.byID[id]
	u.Status = userdomain.StatusSuspended
	users.byID[id] = u
	users.byEmail[u.Email] = u
}

func TestConfirmEmailChangeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *emailChangeFixture)
		wantErr error
	}{
		{name: "token lookup", setup: func(f *emailChangeFixture) { f.resets.fail = faults{"FindByHash": errBoom} }, wantErr: errBoom},
		{name: "account suspended since the request", setup: func(f *emailChangeFixture) { suspend(f.users, f.user.UUID) }, wantErr: authdomain.ErrUserInactive},
		{name: "availability lookup", setup: func(f *emailChangeFixture) { f.users.fail = faults{"FindByEmail": errBoom} }, wantErr: errBoom},
		{name: "update email", setup: func(f *emailChangeFixture) { f.users.fail = faults{"UpdateEmail": errBoom} }, wantErr: errBoom},
		{name: "spend token", setup: func(f *emailChangeFixture) { f.resets.fail = faults{"MarkUsed": errBoom} }, wantErr: errBoom},
		{name: "revoke sessions", setup: func(f *emailChangeFixture) { f.sessions.fail = faults{"RevokeAllForUser": errBoom} }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newEmailChangeFixture(t)

			_, err := f.svc.RequestEmailChange(t.Context(), f.user.UUID, "new@example.com", "Secret123456")
			require.NoError(t, err)

			tt.setup(f)

			user, err := f.svc.ConfirmEmailChange(t.Context(), f.user.UUID, f.notifier.token)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, userdomain.User{}, user)
			assert.Empty(t, f.notifier.changed, "no change notice")
			assert.Zero(t, f.cache.Invalidations(readcache.Users))
		})
	}
}

func TestConfirmEmailChangeToAddressTheUserAlreadyHolds(t *testing.T) {
	t.Parallel()

	f := newEmailChangeFixture(t)
	ctx := t.Context()

	_, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "new@example.com", "Secret123456")
	require.NoError(t, err)

	f.users.byEmail["new@example.com"] = f.users.byID[f.user.UUID]

	user, err := f.svc.ConfirmEmailChange(ctx, f.user.UUID, f.notifier.token)
	require.NoError(t, err, "an address held by the same account is available to it")
	assert.Equal(t, "new@example.com", user.Email)
}

func TestConfirmEmailChangeRejectsExpiredAndForeignTokens(t *testing.T) {
	t.Parallel()

	f := newEmailChangeFixture(t)
	ctx := t.Context()

	_, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "new@example.com", "Secret123456")
	require.NoError(t, err)

	f.clock.t = f.clock.t.Add(2 * time.Hour)
	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, f.notifier.token)
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenExpired)

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, "unknown")
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenInvalid)

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, " ")
	require.ErrorIs(t, err, authdomain.ErrValidation)
}

func TestConfirmEmailChangeRechecksAvailability(t *testing.T) {
	t.Parallel()

	f := newEmailChangeFixture(t)
	ctx := t.Context()

	_, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "new@example.com", "Secret123456")
	require.NoError(t, err)

	squatter := userdomain.User{UUID: uuid.New(), Email: "new@example.com", Status: userdomain.StatusActive}
	f.users.byEmail[squatter.Email] = squatter

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, f.notifier.token)
	require.ErrorIs(t, err, authdomain.ErrEmailTaken)
}

func TestTokensAreNotInterchangeableAcrossPurposes(t *testing.T) {
	t.Parallel()

	f := newEmailChangeFixture(t)
	ctx := t.Context()

	_, err := f.svc.RequestEmailChange(ctx, f.user.UUID, "new@example.com", "Secret123456")
	require.NoError(t, err)

	validity, err := f.svc.CheckResetToken(ctx, f.notifier.token)
	require.NoError(t, err)
	require.False(t, validity.Valid)

	err = f.svc.ResetPassword(ctx, f.notifier.token, "NewSecret123456", "NewSecret123456")
	require.ErrorIs(t, err, authdomain.ErrInvalidToken, "an email change token cannot reset the password")

	sink := &capturingSink{}
	reset := authservice.New(f.users, f.sessions, f.resets, fakeHasher{}, fakeTokens{}, f.clock, uuidGen{}, authservice.Config{}, sink)
	require.NoError(t, reset.ForgotPassword(ctx, "old@example.com"))

	_, err = f.svc.ConfirmEmailChange(ctx, f.user.UUID, sink.raw)
	require.ErrorIs(t, err, authservice.ErrEmailChangeTokenInvalid, "a password reset token cannot change the email")
}

func TestMapEmailChangeError(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]string{
		authservice.ErrEmailChangeTokenInvalid: "auth.email.change_token_invalid",
		authservice.ErrEmailChangeTokenUsed:    "auth.email.change_token_used",
		authservice.ErrEmailChangeTokenExpired: "auth.email.change_token_expired",
		authdomain.ErrEmailTaken:               "auth.email.taken",
		authdomain.ErrCurrentPassword:          "password.current_mismatch",
	} {
		code, _, _ := authservice.MapEmailChangeError(err)
		require.Equal(t, want, code)
	}
}
