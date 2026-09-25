package handlers

import (
	"context"
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
}
