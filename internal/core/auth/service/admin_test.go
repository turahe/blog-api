package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type memRoles struct {
	known    map[string]bool
	assigned map[uuid.UUID][]string
}

func newMemRoles(names ...string) *memRoles {
	r := &memRoles{known: map[string]bool{}, assigned: map[uuid.UUID][]string{}}
	for _, n := range names {
		r.known[n] = true
	}

	return r
}

func (r *memRoles) CheckRoles(_ context.Context, names []string) error {
	for _, n := range names {
		if !r.known[n] {
			return rbacdomain.ErrRoleNotFound
		}
	}

	return nil
}

func (r *memRoles) AssignRoles(_ context.Context, userID uuid.UUID, names []string) error {
	r.assigned[userID] = append(r.assigned[userID], names...)
	return nil
}

func validNewUser() authdomain.NewUser {
	return authdomain.NewUser{
		Email: " Ada@Example.com ", Username: "ada", FullName: "Ada Lovelace", Password: "Correct-Horse-9",
	}
}

func TestAdminCreateUserCreatesActiveAccountWithRoles(t *testing.T) {
	t.Parallel()

	users, sessions, resets := newMemStores()
	roles := newMemRoles("editor")
	svc := newService(users, sessions, resets, nil).WithRoles(roles)

	in := validNewUser()
	in.Roles = []string{"editor"}

	user, err := svc.AdminCreateUser(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "ada@example.com", user.Email)
	require.Equal(t, userdomain.StatusActive, user.Status)
	require.Equal(t, "hash:Correct-Horse-9", users.byID[user.UUID].PasswordHash)
	require.Equal(t, []string{"editor"}, roles.assigned[user.UUID])
}

func TestAdminCreateUserRejectsBadInput(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(*authdomain.NewUser)
		want   error
	}{
		"email":     {func(in *authdomain.NewUser) { in.Email = "not-an-email" }, authdomain.ErrValidation},
		"username":  {func(in *authdomain.NewUser) { in.Username = "a b" }, authdomain.ErrValidation},
		"full name": {func(in *authdomain.NewUser) { in.FullName = "  " }, authdomain.ErrValidation},
		"password":  {func(in *authdomain.NewUser) { in.Password = "short" }, authdomain.ErrPasswordStrength},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			users, sessions, resets := newMemStores()
			in := validNewUser()
			tc.mutate(&in)

			_, err := newService(users, sessions, resets, nil).AdminCreateUser(context.Background(), in)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, users.byID)
		})
	}
}

func TestAdminCreateUserRejectsTakenIdentity(t *testing.T) {
	t.Parallel()

	existing := userdomain.User{UUID: uuid.New(), Email: "ada@example.com", Username: "ada", Status: userdomain.StatusActive}

	users, sessions, resets := newMemStores(existing)
	svc := newService(users, sessions, resets, nil)

	_, err := svc.AdminCreateUser(context.Background(), validNewUser())
	require.ErrorIs(t, err, authdomain.ErrEmailTaken)

	in := validNewUser()
	in.Email = "other@example.com"
	_, err = svc.AdminCreateUser(context.Background(), in)
	require.ErrorIs(t, err, userdomain.ErrUsernameTaken)
}

func TestAdminCreateUserRejectsUnknownRoleBeforeCreating(t *testing.T) {
	t.Parallel()

	users, sessions, resets := newMemStores()
	svc := newService(users, sessions, resets, nil).WithRoles(newMemRoles("editor"))

	in := validNewUser()
	in.Roles = []string{"overlord"}

	_, err := svc.AdminCreateUser(context.Background(), in)
	require.ErrorIs(t, err, rbacdomain.ErrRoleNotFound)
	require.Empty(t, users.byID)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 422, status)
}

func TestAdminResetPasswordIssuesLinkAndRevokesSessions(t *testing.T) {
	t.Parallel()

	user := userdomain.User{UUID: uuid.New(), Email: "ada@example.com", Username: "ada", Status: userdomain.StatusActive}
	users, sessions, resets := newMemStores(user)

	stale := authdomain.PasswordResetToken{UUID: uuid.New(), UserUUID: user.UUID, Purpose: authdomain.PurposePasswordReset, TokenHash: "old"}
	resets.byID[stale.UUID], resets.byHash[stale.TokenHash] = stale, stale

	session := authdomain.RefreshSession{UUID: uuid.New(), UserUUID: user.UUID, TokenHash: "s", ExpiresAt: time.Now().Add(time.Hour)}
	sessions.byID[session.UUID], sessions.byHash[session.TokenHash] = session, session

	sink := &capturingSink{}
	result, err := newService(users, sessions, resets, sink).AdminResetPassword(context.Background(), user.UUID, true)
	require.NoError(t, err)
	require.True(t, result.SessionsRevoked)
	require.False(t, result.ExpiresAt.IsZero())
	require.NotEmpty(t, sink.raw)
	require.NotNil(t, resets.byID[stale.UUID].UsedAt, "earlier links are invalidated")
	require.NotNil(t, sessions.byID[session.UUID].RevokedAt)
}

func TestAdminResetPasswordCanKeepSessions(t *testing.T) {
	t.Parallel()

	user := userdomain.User{UUID: uuid.New(), Email: "ada@example.com", Username: "ada", Status: userdomain.StatusActive}
	users, sessions, resets := newMemStores(user)

	session := authdomain.RefreshSession{UUID: uuid.New(), UserUUID: user.UUID, TokenHash: "s", ExpiresAt: time.Now().Add(time.Hour)}
	sessions.byID[session.UUID], sessions.byHash[session.TokenHash] = session, session

	result, err := newService(users, sessions, resets, &capturingSink{}).AdminResetPassword(context.Background(), user.UUID, false)
	require.NoError(t, err)
	require.False(t, result.SessionsRevoked)
	require.Nil(t, sessions.byID[session.UUID].RevokedAt)
}

func TestAdminResetPasswordRequiresActiveUser(t *testing.T) {
	t.Parallel()

	suspended := userdomain.User{UUID: uuid.New(), Email: "s@example.com", Username: "s", Status: userdomain.StatusSuspended}
	users, sessions, resets := newMemStores(suspended)
	svc := newService(users, sessions, resets, nil)

	_, err := svc.AdminResetPassword(context.Background(), suspended.UUID, true)
	require.ErrorIs(t, err, authdomain.ErrTargetInactive)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 409, status)

	_, err = svc.AdminResetPassword(context.Background(), uuid.New(), true)
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}
