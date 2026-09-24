package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeEnforcer struct {
	allowed map[uuid.UUID]bool
	err     error
	asked   string
}

func (f *fakeEnforcer) Enforce(_ context.Context, userID uuid.UUID, permission string) (bool, error) {
	f.asked = permission
	return f.allowed[userID], f.err
}

func adminLoginFixture(t *testing.T) (*authservice.AuthService, *memAttempts, userdomain.User) {
	t.Helper()

	user := userdomain.User{
		UUID: uuid.New(), Email: "staff@example.com", Username: "staff", FullName: "Staff",
		PasswordHash: "hash:" + tfPassword, Status: userdomain.StatusActive,
	}
	users, sessions, resets := newMemStores(user)
	attempts := newMemAttempts(3)
	svc := newService(users, sessions, resets, nil).WithLoginAttempts(attempts)

	return svc, attempts, user
}

func TestAdminLoginAllowsStaff(t *testing.T) {
	t.Parallel()

	svc, _, user := adminLoginFixture(t)
	enforcer := &fakeEnforcer{allowed: map[uuid.UUID]bool{user.UUID: true}}
	svc.WithAccessCheck(enforcer)

	res, err := svc.AdminLogin(t.Context(), user.Email, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)
	require.NotEmpty(t, res.Tokens.AccessToken)
	require.Equal(t, authservice.AdminAccessPermission, enforcer.asked)
}

func TestAdminLoginRejectsNonStaffLikeAWrongPassword(t *testing.T) {
	t.Parallel()

	svc, attempts, user := adminLoginFixture(t)
	svc.WithAccessCheck(&fakeEnforcer{})

	_, err := svc.AdminLogin(t.Context(), user.Email, tfPassword, "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
	require.Zero(t, attempts.failures["email:"+user.Email], "a correct password is not a lockout failure")

	res, err := svc.Login(t.Context(), user.Email, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err, "the regular login is unaffected")
	require.NotEmpty(t, res.Tokens.AccessToken)
}

func TestAdminLoginChecksPasswordBeforeAccess(t *testing.T) {
	t.Parallel()

	svc, attempts, user := adminLoginFixture(t)
	enforcer := &fakeEnforcer{allowed: map[uuid.UUID]bool{user.UUID: true}}
	svc.WithAccessCheck(enforcer)

	_, err := svc.AdminLogin(t.Context(), user.Email, "wrong", "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials)
	require.Empty(t, enforcer.asked, "RBAC is not consulted for a wrong password")
	require.Equal(t, 1, attempts.failures["email:"+user.Email])
}

func TestAdminLoginFailsClosed(t *testing.T) {
	t.Parallel()

	svc, _, user := adminLoginFixture(t)

	_, err := svc.AdminLogin(t.Context(), user.Email, tfPassword, "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrInvalidCredentials, "no access checker means nobody passes")

	boom := errors.New("casbin down")
	svc.WithAccessCheck(&fakeEnforcer{err: boom})

	_, err = svc.AdminLogin(t.Context(), user.Email, tfPassword, "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, boom)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 500, status)
}

func TestAdminLoginStillRequiresTwoFactor(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)
	f.svc.WithAccessCheck(&fakeEnforcer{allowed: map[uuid.UUID]bool{f.userID: true}})

	res, err := f.svc.AdminLogin(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)
	require.NotNil(t, res.Challenge)
	require.Empty(t, res.Tokens.AccessToken)
}
