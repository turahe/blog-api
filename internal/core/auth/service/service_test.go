package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type memUsers struct {
	byEmail map[string]userdomain.User
	byID    map[uuid.UUID]userdomain.User
}

func (m *memUsers) FindByEmail(_ context.Context, email string) (userdomain.User, error) {
	u, ok := m.byEmail[email]
	if !ok {
		return userdomain.User{}, authdomain.ErrInvalidCredentials
	}
	return u, nil
}
func (m *memUsers) FindByID(_ context.Context, id uuid.UUID) (userdomain.User, error) {
	u, ok := m.byID[id]
	if !ok {
		return userdomain.User{}, authdomain.ErrInvalidCredentials
	}
	return u, nil
}
func (m *memUsers) FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error) {
	return m.FindByEmail(ctx, identity)
}
func (m *memUsers) RecordLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	u := m.byID[id]
	u.LastLoginAt = &at
	u.LoginCount++
	m.byID[id] = u
	m.byEmail[u.Email] = u
	return nil
}
func (m *memUsers) Create(_ context.Context, user userdomain.User) (userdomain.User, error) {
	m.byID[user.UUID] = user
	m.byEmail[user.Email] = user
	return user, nil
}
func (m *memUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string, changedAt time.Time) error {
	u := m.byID[id]
	u.PasswordHash = hash
	u.PasswordChangedAt = &changedAt
	m.byID[id] = u
	m.byEmail[u.Email] = u
	return nil
}

type memSessions struct {
	byHash map[string]authdomain.RefreshSession
	byID   map[uuid.UUID]authdomain.RefreshSession
}

func (m *memSessions) Create(_ context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error) {
	m.byHash[session.TokenHash] = session
	m.byID[session.UUID] = session
	return session, nil
}
func (m *memSessions) FindByTokenHash(_ context.Context, hash string) (authdomain.RefreshSession, error) {
	s, ok := m.byHash[hash]
	if !ok {
		return authdomain.RefreshSession{}, authdomain.ErrInvalidToken
	}
	return s, nil
}
func (m *memSessions) Revoke(_ context.Context, id uuid.UUID, at time.Time) error {
	s := m.byID[id]
	s.RevokedAt = &at
	m.byID[id] = s
	m.byHash[s.TokenHash] = s
	return nil
}
func (m *memSessions) RevokeFamily(_ context.Context, userID, familyID uuid.UUID, at time.Time) error {
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
}

func (m *memResets) Create(_ context.Context, token authdomain.PasswordResetToken) error {
	m.byHash[token.TokenHash] = token
	m.byID[token.UUID] = token
	return nil
}
func (m *memResets) FindByHash(_ context.Context, hash string) (authdomain.PasswordResetToken, error) {
	t, ok := m.byHash[hash]
	if !ok {
		return authdomain.PasswordResetToken{}, authdomain.ErrInvalidToken
	}
	return t, nil
}
func (m *memResets) MarkUsed(_ context.Context, id uuid.UUID, at time.Time) error {
	t := m.byID[id]
	t.UsedAt = &at
	m.byID[id] = t
	m.byHash[t.TokenHash] = t
	return nil
}

type capturingSink struct{ raw string }

func (c *capturingSink) Capture(raw string) { c.raw = raw }

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
func (fakeHasher) Compare(hash, password string) bool   { return hash == "hash:"+password }

type fakeTokens struct{}

func (fakeTokens) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	return "access:" + claims.Subject.String(), nil
}
func (fakeTokens) ParseAccess(token string) (authdomain.AccessClaims, error) {
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

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type uuidGen struct{}

func (uuidGen) New() uuid.UUID { return uuid.New() }

func newService(users *memUsers, sessions *memSessions, resets *memResets, sink *capturingSink) *authservice.AuthService {
	return authservice.New(users, sessions, resets, fakeHasher{}, fakeTokens{}, fixedClock{t: time.Now().UTC()}, uuidGen{}, authservice.Config{}, sink)
}

func TestLoginIssuesTokenPair(t *testing.T) {
	id := uuid.New()
	user := userdomain.User{
		UUID: id, Email: "a@example.com", Username: "a", FullName: "A",
		PasswordHash: "hash:secret", Status: userdomain.StatusActive,
	}
	users := &memUsers{byEmail: map[string]userdomain.User{user.Email: user}, byID: map[uuid.UUID]userdomain.User{user.UUID: user}}
	sessions := &memSessions{byHash: map[string]authdomain.RefreshSession{}, byID: map[uuid.UUID]authdomain.RefreshSession{}}
	resets := &memResets{byHash: map[string]authdomain.PasswordResetToken{}, byID: map[uuid.UUID]authdomain.PasswordResetToken{}}
	svc := newService(users, sessions, resets, nil)

	pair, err := svc.Login(context.Background(), "a@example.com", "secret", "ua", "127.0.0.1", true)
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)
}

func TestLoginRejectsBadPassword(t *testing.T) {
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
