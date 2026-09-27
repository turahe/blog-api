package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	rbacservice "github.com/turahe/blog-api/internal/core/rbac/service"
)

type fakeRoles struct {
	err       error
	created   rbacservice.NewRole
	perms     []string
	name      string
	actor     uuid.UUID
	target    uuid.UUID
	userRoles []string
}

func (f *fakeRoles) role(name string) rbacdomain.Role {
	return rbacdomain.Role{UUID: uuid.New(), Name: name, Permissions: []string{"post.read"}}
}

func (f *fakeRoles) List(context.Context) ([]rbacdomain.Role, error) {
	return []rbacdomain.Role{f.role("admin"), f.role("editor")}, f.err
}

func (f *fakeRoles) Get(_ context.Context, name string) (rbacdomain.Role, error) {
	f.name = name
	return f.role(name), f.err
}

func (f *fakeRoles) Create(_ context.Context, in rbacservice.NewRole) (rbacdomain.Role, error) {
	f.created = in
	return f.role(in.Name), f.err
}

func (f *fakeRoles) Update(_ context.Context, name, _ string) (rbacdomain.Role, error) {
	f.name = name
	return f.role(name), f.err
}

func (f *fakeRoles) Delete(_ context.Context, name string) error {
	f.name = name
	return f.err
}

func (f *fakeRoles) SetPermissions(_ context.Context, name string, keys []string) (rbacdomain.Role, error) {
	f.name, f.perms = name, keys
	return f.role(name), f.err
}

func (f *fakeRoles) Permissions(context.Context) ([]rbacdomain.Permission, error) {
	return []rbacdomain.Permission{{UUID: uuid.New(), Key: "post.read"}}, f.err
}

func (f *fakeRoles) UserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	f.target = userID
	return f.userRoles, f.err
}

func (f *fakeRoles) AssignUserRoles(_ context.Context, userID uuid.UUID, names []string) ([]string, error) {
	f.target = userID
	return names, f.err
}

func (f *fakeRoles) RevokeUserRole(_ context.Context, actor, userID uuid.UUID, name string) ([]string, error) {
	f.actor, f.target, f.name = actor, userID, name
	return []string{}, f.err
}

func TestAdminRolesListHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "lists roles", status: nethttp.StatusOK},
		{name: "records unexpected failure", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, adminRolesListHandler(&fakeRoles{err: tc.err}), profileRequest{method: nethttp.MethodGet, target: "/"})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			if tc.code != "" {
				require.Equal(t, tc.code, errorCode(body))
				return
			}

			list := as[[]any](t, body["data"])
			require.Len(t, list, 2)
			require.Equal(t, "admin", as[map[string]any](t, list[0])["name"])
		})
	}
}

func TestAdminRoleGetHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "returns trimmed role", status: nethttp.StatusOK},
		{name: "maps not found", err: rbacdomain.ErrRoleNotFound, status: nethttp.StatusNotFound, code: "rbac.role.not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			roles := &fakeRoles{err: tc.err}
			w, body := runProfile(t, adminRoleGetHandler(roles), profileRequest{method: nethttp.MethodGet, target: "/", param: " editor "})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "editor", roles.name)

			if tc.code != "" {
				require.Equal(t, tc.code, errorCode(body))
				return
			}

			require.Equal(t, "editor", dataOf(body)["name"])
		})
	}
}

func TestAdminRoleCreate(t *testing.T) {
	t.Parallel()

	roles := &fakeRoles{}
	w, body := runProfile(t, adminRoleCreateHandler(roles), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/roles", contentType: "application/json",
		body: `{"name":"reviewer","description":"Reviews","permissions":["post.read"]}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "reviewer", roles.created.Name)
	require.Equal(t, []string{"post.read"}, roles.created.Permissions)
	require.Equal(t, "reviewer", dataOf(body)["name"])
	require.Equal(t, false, dataOf(body)["protected"])

	w, body = runProfile(t, adminRoleCreateHandler(&fakeRoles{err: rbacdomain.ErrRoleExists}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/roles", contentType: "application/json",
		body: `{"name":"editor"}`,
	})
	require.Equal(t, nethttp.StatusConflict, w.Code, w.Body.String())
	require.Equal(t, "rbac.role.exists", errorCode(body))

	roles = &fakeRoles{}
	w, body = runProfile(t, adminRoleCreateHandler(roles), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/roles", contentType: "application/json", body: `{`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "validation_error", errorCode(body))
	require.Empty(t, roles.created.Name, "service not called")
}

func TestAdminRoleUpdateHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "updates description", body: `{"description":"Edits posts"}`, status: nethttp.StatusOK},
		{name: "rejects malformed body", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "maps not found", body: `{"description":"x"}`, err: rbacdomain.ErrRoleNotFound, status: nethttp.StatusNotFound, code: "rbac.role.not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			roles := &fakeRoles{err: tc.err}
			w, body := runProfile(t, adminRoleUpdateHandler(roles), profileRequest{
				method: nethttp.MethodPatch, target: "/", param: "editor", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			if tc.code != "" {
				require.Equal(t, tc.code, errorCode(body))
				return
			}

			require.Equal(t, "editor", roles.name)
			require.Equal(t, "editor", dataOf(body)["name"])
		})
	}
}

func TestAdminRolePermissionsSet(t *testing.T) {
	t.Parallel()

	roles := &fakeRoles{}
	w, _ := runProfile(t, adminRolePermissionsSetHandler(roles), profileRequest{
		method: nethttp.MethodPut, target: "/api/v1/admin/roles/editor/permissions", param: "editor",
		contentType: "application/json", body: `{"permissions":[]}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "editor", roles.name)
	require.Empty(t, roles.perms)

	w, _ = runProfile(t, adminRolePermissionsSetHandler(roles), profileRequest{
		method: nethttp.MethodPut, target: "/api/v1/admin/roles/editor/permissions", param: "editor",
		contentType: "application/json", body: `{}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, "permissions is required, even if empty")

	w, body := runProfile(t, adminRolePermissionsSetHandler(&fakeRoles{err: rbacdomain.ErrRoleProtected}), profileRequest{
		method: nethttp.MethodPut, target: "/api/v1/admin/roles/admin/permissions", param: "admin",
		contentType: "application/json", body: `{"permissions":["post.read"]}`,
	})
	require.Equal(t, nethttp.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, "rbac.role.protected", errorCode(body))
}

func TestAdminPermissionsListHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "lists permissions", status: nethttp.StatusOK},
		{name: "maps failure", err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, adminPermissionsListHandler(&fakeRoles{err: tc.err}), profileRequest{method: nethttp.MethodGet, target: "/"})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			if tc.code != "" {
				require.Equal(t, tc.code, errorCode(body))
				return
			}

			list := as[[]any](t, body["data"])
			require.Len(t, list, 1)
			require.Equal(t, "post.read", as[map[string]any](t, list[0])["key"])
		})
	}
}

func TestAdminUserRolesListHandler(t *testing.T) {
	t.Parallel()

	target := uuid.New()
	tests := []struct {
		name   string
		param  string
		err    error
		status int
		code   string
	}{
		{name: "rejects invalid user id", param: "nope", status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "maps missing user", param: target.String(), err: rbacdomain.ErrUserNotFound, status: nethttp.StatusNotFound, code: "user.not_found"},
		{name: "lists roles", param: target.String(), status: nethttp.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			roles := &fakeRoles{err: tc.err, userRoles: []string{"editor"}}
			w, body := runProfile(t, adminUserRolesListHandler(roles), profileRequest{method: nethttp.MethodGet, target: "/", param: tc.param})
			require.Equal(t, tc.status, w.Code, w.Body.String())

			if tc.code != "" {
				require.Equal(t, tc.code, errorCode(body))
				return
			}

			require.Equal(t, target, roles.target)
			require.Equal(t, target.String(), dataOf(body)["userId"])
			require.Equal(t, []any{"editor"}, dataOf(body)["roles"])
		})
	}
}

func TestAdminRoleDelete(t *testing.T) {
	t.Parallel()

	roles := &fakeRoles{}
	w := runRaw(t, adminRoleDeleteHandler(roles), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/admin/roles/editor", param: "editor",
	})
	require.Equal(t, nethttp.StatusNoContent, w.Code)
	require.Equal(t, "editor", roles.name)

	w, body := runProfile(t, adminRoleDeleteHandler(&fakeRoles{err: rbacdomain.ErrRoleNotFound}), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/admin/roles/ghost", param: "ghost",
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code, w.Body.String())
	require.Equal(t, "rbac.role.not_found", errorCode(body))
}

func TestAdminUserRoleRevokePassesActor(t *testing.T) {
	t.Parallel()

	roles := &fakeRoles{}
	actor, target := uuid.New(), uuid.New()

	w, _ := runProfile(t, adminUserRoleRevokeHandler(roles), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/admin/users/x/roles/editor",
		param: target.String(), param2: "editor", user: &actor,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, actor, roles.actor)
	require.Equal(t, target, roles.target)
	require.Equal(t, "editor", roles.name)

	w, body := runProfile(t, adminUserRoleRevokeHandler(&fakeRoles{err: rbacdomain.ErrSelfRevoke}), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/admin/users/x/roles/admin",
		param: actor.String(), param2: "admin", user: &actor,
	})
	require.Equal(t, nethttp.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, "rbac.role.self_revoke", errorCode(body))
}

func TestAdminUserRoleRevokeRejectsBeforeCallingService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		param  string
		user   *uuid.UUID
		status int
		code   string
	}{
		{name: "invalid user id", param: "nope", user: &testUserID, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "anonymous actor", param: uuid.NewString(), status: nethttp.StatusUnauthorized, code: "unauthorized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			roles := &fakeRoles{}
			w, body := runProfile(t, adminUserRoleRevokeHandler(roles), profileRequest{
				method: nethttp.MethodDelete, target: "/", param: tc.param, param2: "editor", user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Empty(t, roles.name, "service must not be called")
		})
	}
}

func TestAdminUserRolesAssign(t *testing.T) {
	t.Parallel()

	roles := &fakeRoles{}
	target := uuid.New()

	w, body := runProfile(t, adminUserRolesAssignHandler(roles), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/roles", param: target.String(),
		contentType: "application/json", body: `{"roles":["editor"]}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, target.String(), dataOf(body)["userId"])
	require.Equal(t, []any{"editor"}, dataOf(body)["roles"])

	w, _ = runProfile(t, adminUserRolesAssignHandler(roles), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/roles", param: target.String(),
		contentType: "application/json", body: `{"roles":[]}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	w, body = runProfile(t, adminUserRolesAssignHandler(roles), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/roles", param: "nope",
		contentType: "application/json", body: `{"roles":["editor"]}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "Invalid user id", errorMessage(body))

	w, body = runProfile(t, adminUserRolesAssignHandler(&fakeRoles{err: rbacdomain.ErrRoleNotFound}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/users/x/roles", param: target.String(),
		contentType: "application/json", body: `{"roles":["ghost"]}`,
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code)
	require.Equal(t, "rbac.role.not_found", errorCode(body))
}
