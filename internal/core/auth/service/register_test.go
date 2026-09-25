package service_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type memRegistrations struct {
	mu   sync.Mutex
	rows map[string]authdomain.Registration
}

func (m *memRegistrations) Create(_ context.Context, r authdomain.Registration, maxLive int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	live := 0

	for _, row := range m.rows {
		if strings.EqualFold(row.Email, r.Email) && row.ExpiresAt.After(r.CreatedAt) {
			live++
		}
	}

	if live >= maxLive {
		return false, nil
	}

	m.rows[r.TokenHash] = r

	return true, nil
}

func (m *memRegistrations) FindByTokenHash(_ context.Context, hash string) (authdomain.Registration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rows[hash]
	if !ok {
		return authdomain.Registration{}, authdomain.ErrRegistrationTokenInvalid
	}

	return r, nil
}

func (m *memRegistrations) Consume(_ context.Context, tokenHash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	hit, ok := m.rows[tokenHash]
	if !ok {
		return false, nil
	}

	for hash, row := range m.rows {
		if strings.EqualFold(row.Email, hit.Email) {
			delete(m.rows, hash)
		}
	}

	return true, nil
}

type openPolicy struct{ open bool }

func (p *openPolicy) RegistrationOpen(context.Context) (bool, error) { return p.open, nil }

type sentMail struct {
	kind  string
	email string
	token string
}

type chanNotifier struct{ sent chan sentMail }

func (n chanNotifier) AccountVerify(_ context.Context, r authdomain.Registration, raw string) {
	n.sent <- sentMail{kind: "verify", email: r.Email, token: raw}
}

func (n chanNotifier) AccountExists(_ context.Context, u userdomain.User) {
	n.sent <- sentMail{kind: "exists", email: u.Email}
}

func (n chanNotifier) next(t *testing.T) sentMail {
	t.Helper()

	select {
	case m := <-n.sent:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no email sent")
		return sentMail{}
	}
}

func (n chanNotifier) none(t *testing.T) {
	t.Helper()

	select {
	case m := <-n.sent:
		t.Fatalf("unexpected %s email to %s", m.kind, m.email)
	case <-time.After(50 * time.Millisecond):
	}
}

type signupFixture struct {
	svc      *authservice.AuthService
	users    *memUsers
	sessions *memSessions
	regs     *memRegistrations
	policy   *openPolicy
	mail     chanNotifier
	attempts *memAttempts
}

func newSignupFixture(t *testing.T, existing ...userdomain.User) signupFixture {
	t.Helper()

	f := signupFixture{
		users:    &memUsers{byEmail: map[string]userdomain.User{}, byID: map[uuid.UUID]userdomain.User{}},
		sessions: &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}},
		regs:     &memRegistrations{rows: map[string]authdomain.Registration{}},
		policy:   &openPolicy{open: true},
		mail:     chanNotifier{sent: make(chan sentMail, 8)},
		attempts: newMemAttempts(3),
	}

	for _, u := range existing {
		f.users.byEmail[u.Email] = u
		f.users.byID[u.UUID] = u
	}

	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	f.svc = newService(f.users, f.sessions, resets, &capturingSink{}).
		WithRegistration(f.regs, f.policy, f.mail).
		WithLoginAttempts(f.attempts)

	return f
}

func signUp(email, username string) authdomain.SignUp {
	return authdomain.SignUp{Email: email, Username: username, FullName: "New Reader", Password: "Sup3rSecretPass"}
}

func TestRegisterThenVerifyCreatesAVerifiedActiveAccount(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)

	require.NoError(t, f.svc.Register(t.Context(), signUp(" New@Example.com ", "reader")))

	mail := f.mail.next(t)
	require.Equal(t, "verify", mail.kind)
	require.Equal(t, "new@example.com", mail.email)
	require.Empty(t, f.users.byEmail, "no account exists before verification")

	pair, err := f.svc.VerifyEmail(t.Context(), mail.token, "Sup3rSecretPass", "ua", "203.0.113.1")
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)

	user := f.users.byEmail["new@example.com"]
	assert.Equal(t, "reader", user.Username)
	assert.Equal(t, "New Reader", user.FullName)
	assert.Equal(t, userdomain.StatusActive, user.Status)
	assert.NotNil(t, user.EmailVerifiedAt)
	assert.Equal(t, "hash:Sup3rSecretPass", user.PasswordHash)
	assert.Empty(t, f.regs.rows, "the used registration is gone")

	_, err = f.svc.VerifyEmail(t.Context(), mail.token, "Sup3rSecretPass", "ua", "203.0.113.1")
	require.ErrorIs(t, err, authdomain.ErrRegistrationTokenInvalid, "a token works once")
}

func TestVerifyEmailNeedsTheSignUpPassword(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)
	require.NoError(t, f.svc.Register(t.Context(), signUp("new@example.com", "reader")))
	mail := f.mail.next(t)

	_, err := f.svc.VerifyEmail(t.Context(), mail.token, "Other1Password", "ua", "ip")
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
	assert.Empty(t, f.users.byEmail, "the inbox owner alone cannot activate the account")
	assert.Len(t, f.regs.rows, 1, "a wrong password does not spend the token")
	assert.Equal(t, 1, f.attempts.failures["email:new@example.com"], "and counts toward the lockout")
}

func TestRegisterAnswersAlikeForAnExistingEmail(t *testing.T) {
	t.Parallel()

	owner := userdomain.User{UUID: uuid.New(), Email: "taken@example.com", Username: "owner", Status: userdomain.StatusActive}
	f := newSignupFixture(t, owner)

	require.NoError(t, f.svc.Register(t.Context(), signUp("TAKEN@example.com", "newcomer")))

	mail := f.mail.next(t)
	assert.Equal(t, sentMail{kind: "exists", email: "taken@example.com"}, mail, "the owner is told, nothing is created")
	assert.Empty(t, f.regs.rows)
}

func TestRegisterReportsATakenUsername(t *testing.T) {
	t.Parallel()

	owner := userdomain.User{UUID: uuid.New(), Email: "owner@example.com", Username: "reader", Status: userdomain.StatusActive}
	f := newSignupFixture(t, owner)

	require.ErrorIs(t, f.svc.Register(t.Context(), signUp("new@example.com", "reader")), userdomain.ErrUsernameTaken)
	f.mail.none(t)
}

func TestRegisterCapsLiveSignUpsPerAddress(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)

	for i := range 5 {
		require.NoError(t, f.svc.Register(t.Context(), signUp("new@example.com", "reader"+string(rune('a'+i)))))
	}

	for range 3 {
		f.mail.next(t)
	}

	f.mail.none(t)
	assert.Len(t, f.regs.rows, 3)
}

func TestVerifyingOneSignUpDropsTheOthersForTheAddress(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)
	require.NoError(t, f.svc.Register(t.Context(), signUp("new@example.com", "squatter")))
	require.NoError(t, f.svc.Register(t.Context(), authdomain.SignUp{
		Email: "new@example.com", Username: "owner", FullName: "Owner", Password: "0wnersPassword",
	}))

	var token string

	for range 2 {
		if mail := f.mail.next(t); f.regs.rows["reset-hash-"+mail.token].Username == "owner" {
			token = mail.token
		}
	}

	_, err := f.svc.VerifyEmail(t.Context(), token, "0wnersPassword", "ua", "ip")
	require.NoError(t, err)
	assert.Equal(t, "owner", f.users.byEmail["new@example.com"].Username)
	assert.Empty(t, f.regs.rows, "the other sign-up for the address is dropped")
}

func TestExpiredVerificationTokenIsRefused(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)
	f.regs.rows["reset-hash-old"] = authdomain.Registration{
		UUID: uuid.New(), Email: "new@example.com", Username: "reader", FullName: "R",
		PasswordHash: "hash:Sup3rSecretPass", TokenHash: "reset-hash-old",
		ExpiresAt: time.Now().Add(-time.Minute), CreatedAt: time.Now().Add(-25 * time.Hour),
	}

	_, err := f.svc.VerifyEmail(t.Context(), "old", "Sup3rSecretPass", "ua", "ip")
	require.ErrorIs(t, err, authdomain.ErrRegistrationTokenExpired)
	assert.Empty(t, f.users.byEmail)
}

func TestRegistrationClosed(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)
	f.policy.open = false

	require.ErrorIs(t, f.svc.Register(t.Context(), signUp("new@example.com", "reader")), authdomain.ErrRegistrationClosed)

	_, err := f.svc.VerifyEmail(t.Context(), "any", "Sup3rSecretPass", "ua", "ip")
	require.ErrorIs(t, err, authdomain.ErrRegistrationClosed)
	f.mail.none(t)

	unwired := newService(f.users, f.sessions, &memResets{}, &capturingSink{})
	require.ErrorIs(t, unwired.Register(t.Context(), signUp("new@example.com", "reader")), authdomain.ErrRegistrationClosed)
}

func TestRegisterValidatesInput(t *testing.T) {
	t.Parallel()

	f := newSignupFixture(t)

	for name, in := range map[string]authdomain.SignUp{
		"bad email":     {Email: "nope", Username: "reader", FullName: "R", Password: "Sup3rSecretPass"},
		"bad username":  {Email: "a@example.com", Username: "a b", FullName: "R", Password: "Sup3rSecretPass"},
		"no name":       {Email: "a@example.com", Username: "reader", FullName: " ", Password: "Sup3rSecretPass"},
		"weak password": {Email: "a@example.com", Username: "reader", FullName: "R", Password: "short"},
	} {
		err := f.svc.Register(t.Context(), in)
		require.Error(t, err, name)
		require.True(t, isValidation(err), "%s: %v", name, err)
	}

	f.mail.none(t)
}

func isValidation(err error) bool {
	_, _, status := authservice.MapError(err)
	return status == 400 || status == 422
}
