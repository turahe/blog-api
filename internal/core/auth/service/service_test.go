package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// faults injects errors into the in-memory fakes, keyed by port method name.
type faults map[string]error

var errBoom = errors.New("boom")

type memUsers struct {
	byEmail map[string]userdomain.User
	byID    map[uuid.UUID]userdomain.User
	findErr error
	fail    faults
}

func (m *memUsers) FindByEmail(_ context.Context, email string) (userdomain.User, error) {
	if m.findErr != nil {
		return userdomain.User{}, m.findErr
	}

	if err := m.fail["FindByEmail"]; err != nil {
		return userdomain.User{}, err
	}

	u, ok := m.byEmail[email]
	if !ok {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	return u, nil
}

func (m *memUsers) FindByID(_ context.Context, id uuid.UUID) (userdomain.User, error) {
	if m.findErr != nil {
		return userdomain.User{}, m.findErr
	}

	if err := m.fail["FindByID"]; err != nil {
		return userdomain.User{}, err
	}

	u, ok := m.byID[id]
	if !ok {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	return u, nil
}

func (m *memUsers) FindByUsernameOrEmail(_ context.Context, identity string) (userdomain.User, error) {
	if err := m.fail["FindByUsernameOrEmail"]; err != nil {
		return userdomain.User{}, err
	}

	for _, u := range m.byID {
		if u.Username == identity {
			return u, nil
		}
	}

	if m.findErr != nil {
		return userdomain.User{}, m.findErr
	}

	u, ok := m.byEmail[identity]
	if !ok {
		return userdomain.User{}, userdomain.ErrNotFound
	}

	return u, nil
}

func (m *memUsers) RecordLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	if err := m.fail["RecordLogin"]; err != nil {
		return err
	}

	u := m.byID[id]
	u.LastLoginAt = &at
	u.LoginCount++
	m.byID[id] = u
	m.byEmail[u.Email] = u

	return nil
}

func (m *memUsers) Create(_ context.Context, user userdomain.User) (userdomain.User, error) {
	if err := m.fail["Create"]; err != nil {
		return userdomain.User{}, err
	}

	m.byID[user.UUID] = user
	m.byEmail[user.Email] = user

	return user, nil
}

func (m *memUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string, changedAt time.Time) error {
	if err := m.fail["UpdatePassword"]; err != nil {
		return err
	}

	u := m.byID[id]
	u.PasswordHash = hash
	u.PasswordChangedAt = &changedAt
	m.byID[id] = u
	m.byEmail[u.Email] = u

	return nil
}

func (m *memUsers) UpdateEmail(_ context.Context, id uuid.UUID, email string, verifiedAt time.Time) error {
	if err := m.fail["UpdateEmail"]; err != nil {
		return err
	}

	u := m.byID[id]
	delete(m.byEmail, u.Email)
	u.Email = email
	u.EmailVerifiedAt = &verifiedAt
	m.byID[id] = u
	m.byEmail[email] = u

	return nil
}

type memSessions struct {
	byHash          map[string]authdomain.RefreshSession
	byID            map[uuid.UUID]authdomain.RefreshSession
	revokeFamilyErr error
	fail            faults
}

func (m *memSessions) Create(_ context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error) {
	if err := m.fail["Create"]; err != nil {
		return authdomain.RefreshSession{}, err
	}

	m.byHash[session.TokenHash] = session
	m.byID[session.UUID] = session

	return session, nil
}

func (m *memSessions) FindByTokenHash(_ context.Context, hash string) (authdomain.RefreshSession, error) {
	if err := m.fail["FindByTokenHash"]; err != nil {
		return authdomain.RefreshSession{}, err
	}

	s, ok := m.byHash[hash]
	if !ok {
		return authdomain.RefreshSession{}, authdomain.ErrInvalidToken
	}

	return s, nil
}

func (m *memSessions) Revoke(_ context.Context, id uuid.UUID, at time.Time) error {
	if err := m.fail["Revoke"]; err != nil {
		return err
	}

	s := m.byID[id]
	s.RevokedAt = &at
	m.byID[id] = s
	m.byHash[s.TokenHash] = s

	return nil
}

func (m *memSessions) RevokeFamily(_ context.Context, userID, familyID uuid.UUID, at time.Time) error {
	if m.revokeFamilyErr != nil {
		return m.revokeFamilyErr
	}

	for id, s := range m.byID {
		if s.UserUUID == userID && s.FamilyID == familyID {
			s.RevokedAt = &at
			m.byID[id] = s
			m.byHash[s.TokenHash] = s
		}
	}

	return nil
}

func (m *memSessions) RevokeAllForUser(_ context.Context, userID uuid.UUID, at time.Time) error {
	if err := m.fail["RevokeAllForUser"]; err != nil {
		return err
	}

	for id, s := range m.byID {
		if s.UserUUID == userID {
			s.RevokedAt = &at
			m.byID[id] = s
			m.byHash[s.TokenHash] = s
		}
	}

	return nil
}

func (m *memSessions) Replace(_ context.Context, oldID, newID uuid.UUID, at time.Time) error {
	if err := m.fail["Replace"]; err != nil {
		return err
	}

	s := m.byID[oldID]
	s.RevokedAt = &at
	s.ReplacedByUUID = &newID
	m.byID[oldID] = s
	m.byHash[s.TokenHash] = s

	return nil
}

type memResets struct {
	byHash map[string]authdomain.PasswordResetToken
	byID   map[uuid.UUID]authdomain.PasswordResetToken
	fail   faults
}

func (m *memResets) Create(_ context.Context, token authdomain.PasswordResetToken) error {
	if err := m.fail["Create"]; err != nil {
		return err
	}

	m.byHash[token.TokenHash] = token
	m.byID[token.UUID] = token

	return nil
}

func (m *memResets) FindByHash(_ context.Context, hash string) (authdomain.PasswordResetToken, error) {
	if err := m.fail["FindByHash"]; err != nil {
		return authdomain.PasswordResetToken{}, err
	}

	t, ok := m.byHash[hash]
	if !ok {
		return authdomain.PasswordResetToken{}, authdomain.ErrInvalidToken
	}

	return t, nil
}

func (m *memResets) MarkUsed(_ context.Context, id uuid.UUID, at time.Time) error {
	if err := m.fail["MarkUsed"]; err != nil {
		return err
	}

	t := m.byID[id]
	t.UsedAt = &at
	m.byID[id] = t
	m.byHash[t.TokenHash] = t

	return nil
}

func (m *memResets) RevokePending(_ context.Context, userID uuid.UUID, purpose string, at time.Time) error {
	if err := m.fail["RevokePending"]; err != nil {
		return err
	}

	for id, t := range m.byID {
		if t.UserUUID == userID && t.Purpose == purpose && t.UsedAt == nil {
			t.UsedAt = &at
			m.byID[id] = t
			m.byHash[t.TokenHash] = t
		}
	}

	return nil
}

type capturingSink struct{ raw string }

func (c *capturingSink) Capture(raw string) { c.raw = raw }

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
func (fakeHasher) Compare(hash, password string) bool   { return hash == "hash:"+password }

type fakeTokens struct{}

func (fakeTokens) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	return "access:" + claims.Subject.String() + ":" + claims.FamilyID.String(), nil
}

func (fakeTokens) ParseAccess(string) (authdomain.AccessClaims, error) {
	return authdomain.AccessClaims{}, authdomain.ErrInvalidToken
}

func (fakeTokens) IssueRefresh() (string, string, error) {
	raw := "refresh-raw-" + uuid.NewString()
	return raw, "hash-" + raw, nil
}
func (fakeTokens) HashRefresh(raw string) string { return "hash-" + raw }
func (fakeTokens) IssueResetToken() (string, string, string, error) {
	raw := "reset-raw-" + uuid.NewString()
	return raw, "reset-hash-" + raw, uuid.NewString(), nil
}
func (fakeTokens) HashResetToken(raw string) string { return "reset-hash-" + raw }

// failingTokens fails the issuing methods named in fail and parses every token as claims.
type failingTokens struct {
	fakeTokens

	fail   faults
	claims authdomain.AccessClaims
}

func (f failingTokens) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	if err := f.fail["IssueAccess"]; err != nil {
		return "", err
	}

	return f.fakeTokens.IssueAccess(claims)
}

func (f failingTokens) ParseAccess(string) (authdomain.AccessClaims, error) { return f.claims, nil }

func (f failingTokens) IssueRefresh() (string, string, error) {
	if err := f.fail["IssueRefresh"]; err != nil {
		return "", "", err
	}

	return f.fakeTokens.IssueRefresh()
}

func (f failingTokens) IssueResetToken() (string, string, string, error) {
	if err := f.fail["IssueResetToken"]; err != nil {
		return "", "", "", err
	}

	return f.fakeTokens.IssueResetToken()
}

// failingHasher cannot hash.
type failingHasher struct{ fakeHasher }

func (failingHasher) Hash(string) (string, error) { return "", errBoom }

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type uuidGen struct{}

func (uuidGen) New() uuid.UUID { return uuid.New() }

func newService(users *memUsers, sessions *memSessions, resets *memResets, sink *capturingSink) *authservice.AuthService {
	return authservice.New(users, sessions, resets, fakeHasher{}, fakeTokens{}, fixedClock{t: time.Now().UTC()}, uuidGen{}, authservice.Config{}, sink)
}

func TestLoginIssuesTokenPair(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	user := userdomain.User{
		UUID: id, Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:secret", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	svc := newService(users, sessions, resets, nil)

	res, err := svc.Login(context.Background(), "a@example.com", "secret", "ua", "127.0.0.1", true)
	require.NoError(t, err)
	require.Nil(t, res.Challenge)
	require.NotEmpty(t, res.Tokens.AccessToken)
	require.NotEmpty(t, res.Tokens.RefreshToken)

	session := sessions.byHash["hash-"+res.Tokens.RefreshToken]
	require.Equal(t, "access:"+id.String()+":"+session.FamilyID.String(), res.Tokens.AccessToken,
		"the access token names the sign-in it belongs to")

	rotated, err := svc.Refresh(context.Background(), res.Tokens.RefreshToken, "ua", "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, res.Tokens.AccessToken, rotated.AccessToken, "rotation keeps the sign-in family")
}

func TestLoginRejectsBadPassword(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	user := userdomain.User{
		UUID: id, Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:secret", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	svc := newService(users, sessions, resets, nil)

	_, err := svc.Login(context.Background(), "a@example.com", "wrong", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
}

func TestLoginAfterResetWithSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	user := userdomain.User{
		UUID: id, Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:OldPassword1!", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	sink := &capturingSink{}
	svc := newService(users, sessions, resets, sink)

	const spaced = " NewPassword12! "

	require.NoError(t, svc.ForgotPassword(context.Background(), "a@example.com"))
	require.NoError(t, svc.ResetPassword(context.Background(), sink.raw, spaced, spaced))

	_, err := svc.Login(context.Background(), "a@example.com", spaced, "ua", "127.0.0.1", false)
	require.NoError(t, err)

	_, err = svc.Login(context.Background(), "a@example.com", "NewPassword12!", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
}

func TestForgotAndResetPassword(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	user := userdomain.User{
		UUID: id, Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:OldPassword1!", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	sink := &capturingSink{}
	svc := newService(users, sessions, resets, sink)

	require.NoError(t, svc.ForgotPassword(context.Background(), "a@example.com"))
	require.NotEmpty(t, sink.raw)

	validity, err := svc.CheckResetToken(context.Background(), sink.raw)
	require.NoError(t, err)
	require.True(t, validity.Valid)

	require.NoError(t, svc.ResetPassword(context.Background(), sink.raw, "NewPassword12!", "NewPassword12!"))
	require.Equal(t, "hash:NewPassword12!", users.byID[id].PasswordHash)

	validity, err = svc.CheckResetToken(context.Background(), sink.raw)
	require.NoError(t, err)
	require.False(t, validity.Valid)
}

func newMemStores(users ...userdomain.User) (*memUsers, *memSessions, *memResets) {
	mu := &memUsers{byEmail: map[string]userdomain.User{}, byID: map[uuid.UUID]userdomain.User{}}
	for _, u := range users {
		mu.byEmail[u.Email] = u
		mu.byID[u.UUID] = u
	}

	return mu,
		&memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}},
		&memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
}

func TestLoginPropagatesRepositoryFailure(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	users, sessions, resets := newMemStores()
	users.findErr = dbErr

	_, err := newService(users, sessions, resets, nil).
		Login(context.Background(), "a@example.com", "secret", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, dbErr)
	require.NotErrorIs(t, err, authdomain.ErrInvalidCredentials)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 500, status)
}

func TestRefreshReuseReportsFamilyRevokeFailure(t *testing.T) {
	t.Parallel()

	user := userdomain.User{UUID: uuid.New(), Email: "a@example.com", Status: userdomain.StatusActive}
	users, sessions, resets := newMemStores(user)
	revokedAt := time.Now().UTC()
	sessions.byHash["hash-stolen"] = authdomain.RefreshSession{
		UUID: uuid.New(), UserUUID: user.UUID, FamilyID: uuid.New(), TokenHash: "hash-stolen",
		ExpiresAt: revokedAt.Add(time.Hour), RevokedAt: &revokedAt,
	}
	sessions.revokeFamilyErr = errors.New("write failed")

	_, err := newService(users, sessions, resets, nil).Refresh(context.Background(), "stolen", "ua", "127.0.0.1")
	require.ErrorIs(t, err, sessions.revokeFamilyErr)
	require.NotErrorIs(t, err, authdomain.ErrTokenRevoked)
}

func TestRefreshTreatsMissingUserAsInactive(t *testing.T) {
	t.Parallel()

	users, sessions, resets := newMemStores()
	sessions.byHash["hash-orphan"] = authdomain.RefreshSession{
		UUID: uuid.New(), UserUUID: uuid.New(), FamilyID: uuid.New(), TokenHash: "hash-orphan",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	_, err := newService(users, sessions, resets, nil).Refresh(context.Background(), "orphan", "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrUserInactive)
}

func TestRefreshExpiredSessionIsUnauthorized(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)

	f.clock.t = f.clock.t.Add(7*24*time.Hour + time.Second)

	_, err = f.svc.Refresh(t.Context(), res.Tokens.RefreshToken, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrSessionExpired)

	code, _, status := authservice.MapError(err)
	require.Equal(t, "unauthorized", code, "not the password-reset expiry code")
	require.Equal(t, 401, status)
}

func TestRefreshKeepsShortSessionLifetime(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)

	f.clock.t = f.clock.t.Add(6 * 24 * time.Hour)

	pair, err := f.svc.Refresh(t.Context(), res.Tokens.RefreshToken, "ua", "127.0.0.1")
	require.NoError(t, err)

	f.clock.t = f.clock.t.Add(7*24*time.Hour + time.Second)

	_, err = f.svc.Refresh(t.Context(), pair.RefreshToken, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrSessionExpired, "rotation keeps the 7-day lifetime")
}

// countingHasher counts Compare calls to show every login path pays for a hash.
type countingHasher struct {
	fakeHasher

	mu       sync.Mutex
	compares int
}

func (h *countingHasher) Compare(hash, password string) bool {
	h.mu.Lock()
	h.compares++
	h.mu.Unlock()

	return h.fakeHasher.Compare(hash, password)
}

func TestLoginDoesNotRevealAccountState(t *testing.T) {
	t.Parallel()

	suspended := userdomain.User{
		UUID: uuid.New(), Email: "gone@example.com", Username: "gone", FullName: "Gone",
		PasswordHash: "hash:right", Status: userdomain.StatusSuspended,
	}
	users, sessions, resets := newMemStores(suspended)
	hasher := &countingHasher{}
	svc := authservice.New(users, sessions, resets, hasher, fakeTokens{}, fixedClock{t: time.Now().UTC()}, uuidGen{},
		authservice.Config{}, nil)

	_, err := svc.Login(t.Context(), "nobody@example.com", "whatever", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
	require.Equal(t, 1, hasher.compares, "an unknown account still costs a hash")

	_, err = svc.Login(t.Context(), suspended.Email, "wrong", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials, "a suspended account looks like a wrong password")

	_, err = svc.Login(t.Context(), suspended.Email, "right", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrUserInactive, "status is revealed only with the right password")
}

// signalNotifier reports password reset deliveries on a channel.
type signalNotifier struct {
	capturingNotifier

	resets chan string
}

func (n *signalNotifier) PasswordReset(_ context.Context, user userdomain.User, _ string, _ time.Time) {
	n.resets <- user.Email
}

func TestForgotPasswordDeliversInBackground(t *testing.T) {
	t.Parallel()

	user := userdomain.User{
		UUID: uuid.New(), Email: "ada@example.com", Username: "ada", FullName: "Ada",
		PasswordHash: "hash:pw", Status: userdomain.StatusActive,
	}
	users, sessions, resets := newMemStores(user)
	notifier := &signalNotifier{resets: make(chan string)}
	svc := newService(users, sessions, resets, &capturingSink{}).WithEmailChange(notifier, nil)

	require.NoError(t, svc.ForgotPassword(t.Context(), user.Email), "returns without waiting for delivery")

	select {
	case got := <-notifier.resets:
		require.Equal(t, user.Email, got)
	case <-time.After(5 * time.Second):
		t.Fatal("reset email was not delivered")
	}
}

func TestForgotPasswordKeepsOnlyTheNewestToken(t *testing.T) {
	t.Parallel()

	user := userdomain.User{
		UUID: uuid.New(), Email: "ada@example.com", Username: "ada", FullName: "Ada",
		PasswordHash: "hash:pw", Status: userdomain.StatusActive,
	}
	users, sessions, resets := newMemStores(user)
	sink := &capturingSink{}
	svc := newService(users, sessions, resets, sink)
	ctx := t.Context()

	require.NoError(t, svc.ForgotPassword(ctx, user.Email))

	first := sink.raw

	require.NoError(t, svc.ForgotPassword(ctx, user.Email))

	second := sink.raw

	require.ErrorIs(t, svc.ResetPassword(ctx, first, "N3w-Passw0rd!", "N3w-Passw0rd!"), authdomain.ErrTokenUsed,
		"a newer request revokes the older link")
	require.NoError(t, svc.ResetPassword(ctx, second, "N3w-Passw0rd!", "N3w-Passw0rd!"))
	require.ErrorIs(t, svc.ResetPassword(ctx, second, "An0ther-Pass!", "An0ther-Pass!"), authdomain.ErrTokenUsed)
}

var testNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

const oldPassword, newPassword = "Old-Passw0rd!", "N3w-Passw0rd!"

func activeTestUser() userdomain.User {
	return userdomain.User{
		UUID: uuid.New(), Email: "ada@example.com", Username: "ada", FullName: "Ada",
		PasswordHash: "hash:" + oldPassword, Status: userdomain.StatusActive,
	}
}

// newServiceAt builds a service over the stores with the clock fixed at testNow.
func newServiceAt(
	users *memUsers, sessions *memSessions, resets *memResets,
	hasher ports.PasswordHasher, tokens ports.TokenService, sink ports.ResetTokenSink,
) *authservice.AuthService {
	return authservice.New(users, sessions, resets, hasher, tokens, fixedClock{t: testNow}, uuidGen{}, authservice.Config{}, sink)
}

func seedSession(sessions *memSessions, session authdomain.RefreshSession) {
	sessions.byHash[session.TokenHash] = session
	sessions.byID[session.UUID] = session
}

func seedResetToken(resets *memResets, token authdomain.PasswordResetToken) {
	resets.byHash[token.TokenHash] = token
	resets.byID[token.UUID] = token
}

func TestLoginRequiresEmailAndPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, email, password string
	}{
		{name: "no email", password: "pw"},
		{name: "blank email", email: "   ", password: "pw"},
		{name: "no password", email: "ada@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users, sessions, resets := newMemStores(activeTestUser())
			_, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).
				Login(t.Context(), tt.email, tt.password, "ua", "127.0.0.1", false)
			require.ErrorIs(t, err, authdomain.ErrValidation)
		})
	}
}

func TestLoginPropagatesSessionIssueFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		userFaults   faults
		sessionFault faults
		tokenFaults  faults
	}{
		{name: "record login", userFaults: faults{"RecordLogin": errBoom}},
		{name: "issue refresh token", tokenFaults: faults{"IssueRefresh": errBoom}},
		{name: "store session", sessionFault: faults{"Create": errBoom}},
		{name: "issue access token", tokenFaults: faults{"IssueAccess": errBoom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)
			users.fail, sessions.fail = tt.userFaults, tt.sessionFault

			res, err := newServiceAt(users, sessions, resets, fakeHasher{}, failingTokens{fail: tt.tokenFaults}, nil).
				Login(t.Context(), user.Email, oldPassword, "ua", "127.0.0.1", true)
			require.ErrorIs(t, err, errBoom)
			assert.Empty(t, res.Tokens.AccessToken)
			assert.Empty(t, res.Tokens.RefreshToken)
		})
	}
}

func TestRefreshRejectsUnknownTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		token   string
		fail    faults
		wantErr error
	}{
		{name: "blank", token: "   ", wantErr: authdomain.ErrInvalidToken},
		{name: "unknown", token: "nope", wantErr: authdomain.ErrInvalidToken},
		{name: "lookup failure", token: "tok", fail: faults{"FindByTokenHash": errBoom}, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users, sessions, resets := newMemStores(activeTestUser())
			sessions.fail = tt.fail

			_, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).
				Refresh(t.Context(), tt.token, "ua", "127.0.0.1")
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRefreshReuseRevokesTheWholeFamily(t *testing.T) {
	t.Parallel()

	user := activeTestUser()
	users, sessions, resets := newMemStores(user)
	svc := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil)
	ctx := t.Context()

	stolen, err := svc.Login(ctx, user.Email, oldPassword, "ua", "127.0.0.1", true)
	require.NoError(t, err)

	rotated, err := svc.Refresh(ctx, stolen.Tokens.RefreshToken, "ua", "127.0.0.1")
	require.NoError(t, err)

	otherDevice, err := svc.Login(ctx, user.Email, oldPassword, "ua2", "10.0.0.2", true)
	require.NoError(t, err)

	_, err = svc.Refresh(ctx, stolen.Tokens.RefreshToken, "attacker", "203.0.113.9")
	require.ErrorIs(t, err, authdomain.ErrTokenRevoked)

	code, _, status := authservice.MapError(err)
	assert.Equal(t, "unauthorized", code)
	assert.Equal(t, 401, status)

	assert.NotNil(t, sessions.byHash["hash-"+rotated.RefreshToken].RevokedAt, "the rotated successor is revoked too")

	_, err = svc.Refresh(ctx, rotated.RefreshToken, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrTokenRevoked, "the legitimate holder must sign in again")

	assert.Nil(t, sessions.byHash["hash-"+otherDevice.Tokens.RefreshToken].RevokedAt, "other sign-ins survive")

	_, err = svc.Refresh(ctx, otherDevice.Tokens.RefreshToken, "ua2", "10.0.0.2")
	require.NoError(t, err)
}

func TestRefreshPropagatesRotationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sessionFaults faults
		tokenFaults   faults
	}{
		{name: "issue refresh token", tokenFaults: faults{"IssueRefresh": errBoom}},
		{name: "store new session", sessionFaults: faults{"Create": errBoom}},
		{name: "replace old session", sessionFaults: faults{"Replace": errBoom}},
		{name: "issue access token", tokenFaults: faults{"IssueAccess": errBoom}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)
			seedSession(sessions, authdomain.RefreshSession{
				UUID: uuid.New(), UserUUID: user.UUID, FamilyID: uuid.New(), TokenHash: "hash-tok",
				ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow.Add(-time.Hour),
			})
			sessions.fail = tt.sessionFaults

			pair, err := newServiceAt(users, sessions, resets, fakeHasher{}, failingTokens{fail: tt.tokenFaults}, nil).
				Refresh(t.Context(), "tok", "ua", "127.0.0.1")
			require.ErrorIs(t, err, errBoom)
			assert.Empty(t, pair.AccessToken)
		})
	}
}

func TestRefreshCapsInheritedLifetimeAtRefreshTTL(t *testing.T) {
	t.Parallel()

	const refreshTTL = 30 * 24 * time.Hour

	tests := []struct {
		name               string
		createdAt, expires time.Time
	}{
		{name: "unknown creation time", createdAt: time.Time{}, expires: testNow.Add(time.Hour)},
		{name: "longer than the configured ttl", createdAt: testNow.Add(-time.Hour), expires: testNow.Add(60 * 24 * time.Hour)},
		{name: "created after it expires", createdAt: testNow.Add(2 * time.Hour), expires: testNow.Add(time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)
			seedSession(sessions, authdomain.RefreshSession{
				UUID: uuid.New(), UserUUID: user.UUID, FamilyID: uuid.New(), TokenHash: "hash-tok",
				ExpiresAt: tt.expires, CreatedAt: tt.createdAt,
			})

			pair, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).
				Refresh(t.Context(), "tok", "ua", "127.0.0.1")
			require.NoError(t, err)
			assert.Equal(t, testNow.Add(refreshTTL), sessions.byHash["hash-"+pair.RefreshToken].ExpiresAt)
		})
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()

	owner := uuid.New()

	tests := []struct {
		name        string
		caller      uuid.UUID
		token       string
		fail        faults
		wantErr     error
		wantRevoked bool
	}{
		{name: "no token is a no-op", caller: owner, token: "  "},
		{name: "unknown token is ignored", caller: owner, token: "unknown"},
		{name: "lookup failure", caller: owner, token: "tok", fail: faults{"FindByTokenHash": errBoom}, wantErr: errBoom},
		{name: "someone else's session", caller: uuid.New(), token: "tok", wantErr: authdomain.ErrInvalidToken},
		{name: "revoke failure", caller: owner, token: "tok", fail: faults{"Revoke": errBoom}, wantErr: errBoom},
		{name: "own session is revoked", caller: owner, token: " tok ", wantRevoked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users, sessions, resets := newMemStores()
			session := authdomain.RefreshSession{
				UUID: uuid.New(), UserUUID: owner, FamilyID: uuid.New(), TokenHash: "hash-tok", ExpiresAt: testNow.Add(time.Hour),
			}
			seedSession(sessions, session)
			sessions.fail = tt.fail

			err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).Logout(t.Context(), tt.caller, tt.token)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			revokedAt := sessions.byID[session.UUID].RevokedAt
			if !tt.wantRevoked {
				assert.Nil(t, revokedAt)
				return
			}

			require.NotNil(t, revokedAt)
			assert.Equal(t, testNow, *revokedAt)
		})
	}
}

func TestParseAccessTokenDelegatesToTokenService(t *testing.T) {
	t.Parallel()

	users, sessions, resets := newMemStores()
	claims := authdomain.AccessClaims{Subject: uuid.New(), Email: "ada@example.com"}

	got, err := newServiceAt(users, sessions, resets, fakeHasher{}, failingTokens{claims: claims}, nil).ParseAccessToken("raw")
	require.NoError(t, err)
	assert.Equal(t, claims, got)

	_, err = newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).ParseAccessToken("raw")
	require.ErrorIs(t, err, authdomain.ErrInvalidToken)
}

func TestForgotPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identity   string
		suspended  bool
		userFaults faults
		wantErr    error
		wantToken  bool
	}{
		{name: "too short", identity: " ab ", wantErr: authdomain.ErrValidation},
		{name: "unknown account succeeds silently", identity: "nobody@example.com"},
		{name: "lookup failure", identity: "ada", userFaults: faults{"FindByUsernameOrEmail": errBoom}, wantErr: errBoom},
		{name: "inactive account succeeds silently", identity: "ada", suspended: true},
		{name: "by username", identity: "ada", wantToken: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			if tt.suspended {
				user.Status = userdomain.StatusSuspended
			}

			users, sessions, resets := newMemStores(user)
			users.fail = tt.userFaults
			sink := &capturingSink{}

			err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, sink).ForgotPassword(t.Context(), tt.identity)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantToken, sink.raw != "")
			assert.Equal(t, tt.wantToken, len(resets.byID) == 1)
		})
	}
}

func TestForgotPasswordPropagatesIssueFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tokenFaults faults
		resetFaults faults
		recordErr   error
	}{
		{name: "issue token", tokenFaults: faults{"IssueResetToken": errBoom}},
		{name: "revoke pending tokens", resetFaults: faults{"RevokePending": errBoom}},
		{name: "store token", resetFaults: faults{"Create": errBoom}},
		{name: "record event", recordErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)
			resets.fail = tt.resetFaults
			sink := &capturingSink{}

			svc := newServiceAt(users, sessions, resets, fakeHasher{}, failingTokens{fail: tt.tokenFaults}, sink).
				WithEvents((&eventtest.Recorder{Err: tt.recordErr}).Unit())

			require.ErrorIs(t, svc.ForgotPassword(t.Context(), user.Email), errBoom)
			assert.Empty(t, sink.raw, "no token is delivered when issuing fails")
		})
	}
}

func TestCheckResetToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		expiresAt time.Time
		fail      faults
		wantErr   error
		want      authdomain.ResetTokenValidity
	}{
		{name: "blank", raw: " ", wantErr: authdomain.ErrValidation},
		{name: "unknown", raw: "other", expiresAt: testNow.Add(time.Hour), want: authdomain.ResetTokenValidity{}},
		{name: "lookup failure", raw: "raw", fail: faults{"FindByHash": errBoom}, wantErr: errBoom},
		{
			name: "expired", raw: "raw", expiresAt: testNow,
			want: authdomain.ResetTokenValidity{Valid: false, ExpiresAt: testNow},
		},
		{
			name: "usable", raw: " raw ", expiresAt: testNow.Add(time.Hour),
			want: authdomain.ResetTokenValidity{Valid: true, ExpiresAt: testNow.Add(time.Hour)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users, sessions, resets := newMemStores()
			seedResetToken(resets, authdomain.PasswordResetToken{
				UUID: uuid.New(), UserUUID: uuid.New(), TokenHash: "reset-hash-raw",
				Purpose: authdomain.PurposePasswordReset, ExpiresAt: tt.expiresAt,
			})
			resets.fail = tt.fail

			got, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).CheckResetToken(t.Context(), tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResetPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		raw               string
		password, confirm string
		expiresAt         time.Time
		hasher            ports.PasswordHasher
		userFaults        faults
		resetFaults       faults
		sessionFaults     faults
		wantErr           error
	}{
		{name: "confirmation mismatch", raw: "raw", password: newPassword, confirm: newPassword + "x", wantErr: authdomain.ErrPasswordMismatch},
		{name: "too short", raw: "raw", password: "Sh0rt", confirm: "Sh0rt", wantErr: authdomain.ErrPasswordStrength},
		{name: "too long", raw: "raw", password: "Aa1" + strings.Repeat("x", 126), confirm: "Aa1" + strings.Repeat("x", 126), wantErr: authdomain.ErrPasswordStrength},
		{name: "no upper case", raw: "raw", password: "n3w-passw0rd!", confirm: "n3w-passw0rd!", wantErr: authdomain.ErrPasswordStrength},
		{name: "no lower case", raw: "raw", password: "N3W-PASSW0RD!", confirm: "N3W-PASSW0RD!", wantErr: authdomain.ErrPasswordStrength},
		{name: "no digit", raw: "raw", password: "New-Password!", confirm: "New-Password!", wantErr: authdomain.ErrPasswordStrength},
		{name: "unknown token", raw: "other", password: newPassword, confirm: newPassword, wantErr: authdomain.ErrInvalidToken},
		{name: "token lookup failure", raw: "raw", password: newPassword, confirm: newPassword, resetFaults: faults{"FindByHash": errBoom}, wantErr: errBoom},
		{name: "expired token", raw: "raw", password: newPassword, confirm: newPassword, expiresAt: testNow, wantErr: authdomain.ErrTokenExpired},
		{name: "hash failure", raw: "raw", password: newPassword, confirm: newPassword, hasher: failingHasher{}, wantErr: errBoom},
		{name: "update failure", raw: "raw", password: newPassword, confirm: newPassword, userFaults: faults{"UpdatePassword": errBoom}, wantErr: errBoom},
		{name: "spend tokens failure", raw: "raw", password: newPassword, confirm: newPassword, resetFaults: faults{"RevokePending": errBoom}, wantErr: errBoom},
		{name: "revoke sessions failure", raw: "raw", password: newPassword, confirm: newPassword, sessionFaults: faults{"RevokeAllForUser": errBoom}, wantErr: errBoom},
		{name: "success", raw: " raw ", password: newPassword, confirm: newPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)

			expiresAt := tt.expiresAt
			if expiresAt.IsZero() {
				expiresAt = testNow.Add(time.Hour)
			}

			seedResetToken(resets, authdomain.PasswordResetToken{
				UUID: uuid.New(), UserUUID: user.UUID, TokenHash: "reset-hash-raw",
				Purpose: authdomain.PurposePasswordReset, ExpiresAt: expiresAt,
			})
			seedSession(sessions, authdomain.RefreshSession{UUID: uuid.New(), UserUUID: user.UUID, TokenHash: "s", ExpiresAt: testNow.Add(time.Hour)})
			users.fail, resets.fail, sessions.fail = tt.userFaults, tt.resetFaults, tt.sessionFaults

			hasher := tt.hasher
			if hasher == nil {
				hasher = fakeHasher{}
			}

			err := newServiceAt(users, sessions, resets, hasher, fakeTokens{}, nil).ResetPassword(t.Context(), tt.raw, tt.password, tt.confirm)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "hash:"+newPassword, users.byID[user.UUID].PasswordHash)
			assert.NotNil(t, sessions.byHash["s"].RevokedAt, "every session is revoked")
			assert.NotNil(t, resets.byHash["reset-hash-raw"].UsedAt, "the token is spent")
		})
	}
}

// passwordChangeNotifier records password change notices.
type passwordChangeNotifier struct {
	capturingNotifier

	changed []uuid.UUID
}

func (n *passwordChangeNotifier) PasswordChanged(_ context.Context, user userdomain.User) {
	n.changed = append(n.changed, user.UUID)
}

func TestChangePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		unknownUser       bool
		current           string
		password, confirm string
		revokeAll         bool
		hasher            ports.PasswordHasher
		userFaults        faults
		sessionFaults     faults
		wantErr           error
	}{
		{name: "confirmation mismatch", current: oldPassword, password: newPassword, confirm: "other", wantErr: authdomain.ErrPasswordMismatch},
		{name: "weak password", current: oldPassword, password: "weakpassword", confirm: "weakpassword", wantErr: authdomain.ErrPasswordStrength},
		{name: "unknown user", unknownUser: true, current: oldPassword, password: newPassword, confirm: newPassword, wantErr: authdomain.ErrUserInactive},
		{name: "lookup failure", current: oldPassword, password: newPassword, confirm: newPassword, userFaults: faults{"FindByID": errBoom}, wantErr: errBoom},
		{name: "wrong current password", current: "wrong", password: newPassword, confirm: newPassword, wantErr: authdomain.ErrCurrentPassword},
		{name: "hash failure", current: oldPassword, password: newPassword, confirm: newPassword, hasher: failingHasher{}, wantErr: errBoom},
		{name: "update failure", current: oldPassword, password: newPassword, confirm: newPassword, userFaults: faults{"UpdatePassword": errBoom}, wantErr: errBoom},
		{
			name: "revoke failure", current: oldPassword, password: newPassword, confirm: newPassword, revokeAll: true,
			sessionFaults: faults{"RevokeAllForUser": errBoom}, wantErr: errBoom,
		},
		{name: "keeps sessions", current: oldPassword, password: newPassword, confirm: newPassword},
		{name: "revokes every session", current: oldPassword, password: newPassword, confirm: newPassword, revokeAll: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			users, sessions, resets := newMemStores(user)
			seedSession(sessions, authdomain.RefreshSession{UUID: uuid.New(), UserUUID: user.UUID, TokenHash: "s", ExpiresAt: testNow.Add(time.Hour)})
			users.fail, sessions.fail = tt.userFaults, tt.sessionFaults

			hasher := tt.hasher
			if hasher == nil {
				hasher = fakeHasher{}
			}

			notifier := &passwordChangeNotifier{}
			svc := newServiceAt(users, sessions, resets, hasher, fakeTokens{}, nil).WithEmailChange(notifier, nil)

			userID := user.UUID
			if tt.unknownUser {
				userID = uuid.New()
			}

			changedAt, invalidated, err := svc.ChangePassword(t.Context(), userID, tt.current, tt.password, tt.confirm, tt.revokeAll)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, changedAt)
				assert.False(t, invalidated)
				assert.Empty(t, notifier.changed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testNow, changedAt)
			assert.Equal(t, tt.revokeAll, invalidated)
			assert.Equal(t, "hash:"+newPassword, users.byID[user.UUID].PasswordHash)
			assert.Equal(t, tt.revokeAll, sessions.byHash["s"].RevokedAt != nil)
			assert.Equal(t, []uuid.UUID{user.UUID}, notifier.changed)
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*userdomain.User)
		unknownUser bool
		password    string
		fail        faults
		want        bool
		wantErr     error
	}{
		{name: "current password", password: oldPassword, want: true},
		{name: "wrong password", password: "wrong"},
		{name: "account without a password", mutate: func(u *userdomain.User) { u.PasswordHash = "" }, password: ""},
		{name: "inactive account", mutate: func(u *userdomain.User) { u.Status = userdomain.StatusSuspended }, password: oldPassword, wantErr: authdomain.ErrUserInactive},
		{name: "unknown account", unknownUser: true, password: oldPassword, wantErr: authdomain.ErrUserInactive},
		{name: "lookup failure", password: oldPassword, fail: faults{"FindByID": errBoom}, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := activeTestUser()
			if tt.mutate != nil {
				tt.mutate(&user)
			}

			users, sessions, resets := newMemStores(user)
			users.fail = tt.fail

			userID := user.UUID
			if tt.unknownUser {
				userID = uuid.New()
			}

			ok, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).VerifyPassword(t.Context(), userID, tt.password)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, ok)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, ok)
		})
	}
}

func TestMapResetError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
	}{
		{name: "invalid token", err: authdomain.ErrInvalidToken, wantCode: "auth.password.reset_token_invalid", wantStatus: 400},
		{name: "wrapped invalid token", err: fmt.Errorf("lookup: %w", authdomain.ErrInvalidToken), wantCode: "auth.password.reset_token_invalid", wantStatus: 400},
		{name: "expired token", err: authdomain.ErrTokenExpired, wantCode: "auth.password.reset_token_expired", wantStatus: 400},
		{name: "used token", err: authdomain.ErrTokenUsed, wantCode: "auth.password.reset_token_used", wantStatus: 400},
		{name: "weak password", err: authdomain.ErrPasswordStrength, wantCode: "password.strength", wantStatus: 422},
		{name: "unexpected", err: errBoom, wantCode: "internal_error", wantStatus: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, message, status := authservice.MapResetError(tt.err)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantStatus, status)
			assert.NotEmpty(t, message)
		})
	}
}
