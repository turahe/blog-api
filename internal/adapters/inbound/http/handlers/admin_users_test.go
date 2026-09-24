package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeAdminUsers struct {
	created  authdomain.NewUser
	target   uuid.UUID
	revoke   bool
	calls    int
	err      error
	resetExp time.Time
}

func (f *fakeAdminUsers) AdminCreateUser(_ context.Context, in authdomain.NewUser) (userdomain.User, error) {
	f.calls++
	f.created = in

	if f.err != nil {
		return userdomain.User{}, f.err
	}

	return userdomain.User{UUID: uuid.New(), Email: in.Email, Username: in.Username, FullName: in.FullName, Status: userdomain.StatusActive}, nil
}

func (f *fakeAdminUsers) AdminResetPassword(_ context.Context, userID uuid.UUID, revoke bool) (authdomain.AdminReset, error) {
	f.calls++
	f.target, f.revoke = userID, revoke

	return authdomain.AdminReset{ExpiresAt: f.resetExp, SessionsRevoked: revoke}, f.err
}

func canManage(ok bool) func(*gin.Context) bool {
	return func(*gin.Context) bool { return ok }
}

const newUserBody = `{"email":"ada@example.com","username":"ada","full_name":"Ada","password":"correct-horse-battery"`

func TestAdminCreateUserReturnsCreated(t *testing.T) {
	t.Parallel()

	admin := &fakeAdminUsers{}
	w, body := runProfile(t, adminCreateUserHandler(admin, canManage(false)), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users",
		contentType: "application/json", body: newUserBody + `}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "ada", admin.created.Username)
	require.Empty(t, admin.created.Roles)
	require.Equal(t, "ada@example.com", dataOf(body)["email"])
}

func TestAdminCreateUserRolesRequireRoleManage(t *testing.T) {
	t.Parallel()

	admin := &fakeAdminUsers{}
	w, body := runProfile(t, adminCreateUserHandler(admin, canManage(false)), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users",
		contentType: "application/json", body: newUserBody + `,"roles":["editor"]}`,
	})
	require.Equal(t, nethttp.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, "rbac.forbidden", errorCode(body))
	require.Zero(t, admin.calls)

	w, _ = runProfile(t, adminCreateUserHandler(admin, canManage(true)), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users",
		contentType: "application/json", body: newUserBody + `,"roles":["editor"]}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"editor"}, admin.created.Roles)
}

func TestAdminCreateUserMapsErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"email taken":    {authdomain.ErrEmailTaken, nethttp.StatusConflict, "auth.email.taken"},
		"username taken": {userdomain.ErrUsernameTaken, nethttp.StatusConflict, "user.username.taken"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, adminCreateUserHandler(&fakeAdminUsers{err: tc.err}, canManage(true)), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/admin/users",
				contentType: "application/json", body: newUserBody + `}`,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestAdminCreateUserValidatesBody(t *testing.T) {
	t.Parallel()

	admin := &fakeAdminUsers{}
	w, _ := runProfile(t, adminCreateUserHandler(admin, canManage(true)), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users",
		contentType: "application/json", body: `{"email":"ada@example.com","username":"ada","full_name":"Ada","password":"short"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, w.Body.String())
	require.Zero(t, admin.calls)
}

func TestAdminResetPasswordDefaultsToRevokingSessions(t *testing.T) {
	t.Parallel()

	exp := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	admin := &fakeAdminUsers{resetExp: exp}
	target := uuid.New()

	w, body := runProfile(t, adminResetPasswordHandler(admin), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/password/admin-reset", param: target.String(),
	})
	require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
	require.Equal(t, target, admin.target)
	require.True(t, admin.revoke)
	require.Equal(t, "2026-09-25T12:00:00Z", dataOf(body)["reset_link_expires_at"])
	require.Equal(t, true, dataOf(body)["sessions_revoked"])

	w, _ = runProfile(t, adminResetPasswordHandler(admin), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/password/admin-reset", param: target.String(),
		contentType: "application/json", body: `{"revoke_sessions":false}`,
	})
	require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
	require.False(t, admin.revoke)
}

func TestAdminResetPasswordErrors(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, adminResetPasswordHandler(&fakeAdminUsers{}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/password/admin-reset", param: "not-a-uuid",
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, w.Body.String())

	w, body := runProfile(t, adminResetPasswordHandler(&fakeAdminUsers{err: userdomain.ErrNotFound}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/password/admin-reset", param: uuid.NewString(),
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code, w.Body.String())
	require.Equal(t, "user.not_found", errorCode(body))
}
