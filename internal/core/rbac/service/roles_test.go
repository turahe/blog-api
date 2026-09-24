package service_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/rbac/domain"
	"github.com/turahe/blog-api/internal/core/rbac/service"
)

type memRepo struct {
	roles       map[string]domain.Role
	permissions []string
	users       map[uuid.UUID][]string
}

func newMemRepo() *memRepo {
	return &memRepo{
		roles: map[string]domain.Role{
			"admin":  {Name: "admin", Permissions: []string{"*"}},
			"editor": {Name: "editor", Permissions: []string{"post.read"}},
		},
		permissions: []string{"post.read", "post.create", "user.read"},
		users:       map[uuid.UUID][]string{},
	}
}

func (m *memRepo) CheckRoles(_ context.Context, names []string) error {
	for _, n := range names {
		if _, ok := m.roles[n]; !ok {
			return fmt.Errorf("%w: %s", domain.ErrRoleNotFound, n)
		}
	}

	return nil
}

func (m *memRepo) AssignRoles(ctx context.Context, userID uuid.UUID, names []string) error {
	if err := m.CheckRoles(ctx, names); err != nil {
		return err
	}

	for _, n := range names {
		if !slices.Contains(m.users[userID], n) {
			m.users[userID] = append(m.users[userID], n)
		}
	}

	return nil
}

func (m *memRepo) ListRoles(context.Context) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(m.roles))
	for _, r := range m.roles {
		out = append(out, r)
	}

	return out, nil
}

func (m *memRepo) FindRole(_ context.Context, name string) (domain.Role, error) {
	r, ok := m.roles[name]
	if !ok {
		return domain.Role{}, domain.ErrRoleNotFound
	}

	return r, nil
}

func (m *memRepo) CreateRole(_ context.Context, role domain.Role) (domain.Role, error) {
	if _, ok := m.roles[role.Name]; ok {
		return domain.Role{}, domain.ErrRoleExists
	}

	m.roles[role.Name] = role

	return role, nil
}

func (m *memRepo) UpdateRole(ctx context.Context, name, description string) (domain.Role, error) {
	r, err := m.FindRole(ctx, name)
	if err != nil {
		return r, err
	}

	r.Description = description
	m.roles[name] = r

	return r, nil
}

func (m *memRepo) DeleteRole(ctx context.Context, name string) error {
	if _, err := m.FindRole(ctx, name); err != nil {
		return err
	}

	delete(m.roles, name)

	return nil
}

func (m *memRepo) SetRolePermissions(ctx context.Context, name string, keys []string) (domain.Role, error) {
	r, err := m.FindRole(ctx, name)
	if err != nil {
		return r, err
	}

	r.Permissions = keys
	m.roles[name] = r

	return r, nil
}

func (m *memRepo) ListPermissions(context.Context) ([]domain.Permission, error) {
	out := make([]domain.Permission, len(m.permissions))
	for i, k := range m.permissions {
		out[i] = domain.Permission{Key: k}
	}

	return out, nil
}

func (m *memRepo) CheckPermissions(_ context.Context, keys []string) error {
	for _, k := range keys {
		if !slices.Contains(m.permissions, k) {
			return fmt.Errorf("%w: %s", domain.ErrPermissionNotFound, k)
		}
	}

	return nil
}

func (m *memRepo) UserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	names, ok := m.users[userID]
	if !ok {
		return nil, domain.ErrUserNotFound
	}

	return names, nil
}

func (m *memRepo) RevokeRole(_ context.Context, userID uuid.UUID, name string) error {
	m.users[userID] = slices.DeleteFunc(m.users[userID], func(n string) bool { return n == name })
	return nil
}

func TestCreateRoleValidatesAndDedupes(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := service.NewRoleService(repo)

	role, err := svc.Create(context.Background(), service.NewRole{
		Name: " reviewer ", Description: " Reviews drafts ", Permissions: []string{"post.read", "post.read", " post.create "},
	})
	require.NoError(t, err)
	require.Equal(t, "reviewer", role.Name)
	require.Equal(t, "Reviews drafts", role.Description)
	require.Equal(t, []string{"post.read", "post.create"}, role.Permissions)

	_, err = svc.Create(context.Background(), service.NewRole{Name: "reviewer"})
	require.ErrorIs(t, err, domain.ErrRoleExists)
}

func TestCreateRoleRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   service.NewRole
		want error
	}{
		"uppercase name":     {service.NewRole{Name: "Reviewer"}, domain.ErrValidation},
		"leading digit":      {service.NewRole{Name: "1role"}, domain.ErrValidation},
		"too short":          {service.NewRole{Name: "r"}, domain.ErrValidation},
		"wildcard grant":     {service.NewRole{Name: "sneaky", Permissions: []string{"*"}}, domain.ErrValidation},
		"unknown permission": {service.NewRole{Name: "reviewer", Permissions: []string{"post.nuke"}}, domain.ErrPermissionNotFound},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := service.NewRoleService(newMemRepo()).Create(context.Background(), tc.in)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestProtectedRoleCannotBeDeletedOrRegranted(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := service.NewRoleService(repo)

	require.ErrorIs(t, svc.Delete(context.Background(), domain.ProtectedRole), domain.ErrRoleProtected)

	_, err := svc.SetPermissions(context.Background(), domain.ProtectedRole, []string{"post.read"})
	require.ErrorIs(t, err, domain.ErrRoleProtected)
	require.Equal(t, []string{"*"}, repo.roles["admin"].Permissions)

	require.NoError(t, svc.Delete(context.Background(), "editor"))
	require.NotContains(t, repo.roles, "editor")
}

func TestSetPermissionsReplacesSet(t *testing.T) {
	t.Parallel()

	svc := service.NewRoleService(newMemRepo())

	role, err := svc.SetPermissions(context.Background(), "editor", []string{"user.read"})
	require.NoError(t, err)
	require.Equal(t, []string{"user.read"}, role.Permissions)

	role, err = svc.SetPermissions(context.Background(), "editor", []string{})
	require.NoError(t, err)
	require.Empty(t, role.Permissions)

	_, err = svc.SetPermissions(context.Background(), "missing", nil)
	require.ErrorIs(t, err, domain.ErrRoleNotFound)
}

func TestAssignAndRevokeUserRoles(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := service.NewRoleService(repo)
	user := uuid.New()
	repo.users[user] = []string{}

	names, err := svc.AssignUserRoles(context.Background(), user, []string{"editor", "editor"})
	require.NoError(t, err)
	require.Equal(t, []string{"editor"}, names)

	_, err = svc.AssignUserRoles(context.Background(), user, []string{"ghost"})
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	_, err = svc.AssignUserRoles(context.Background(), uuid.New(), []string{"editor"})
	require.ErrorIs(t, err, domain.ErrUserNotFound)

	_, err = svc.AssignUserRoles(context.Background(), user, nil)
	require.ErrorIs(t, err, domain.ErrValidation)

	names, err = svc.RevokeUserRole(context.Background(), uuid.New(), user, "editor")
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestAdminCannotRevokeOwnAdminRole(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := service.NewRoleService(repo)
	admin := uuid.New()
	repo.users[admin] = []string{"admin"}

	_, err := svc.RevokeUserRole(context.Background(), admin, admin, domain.ProtectedRole)
	require.ErrorIs(t, err, domain.ErrSelfRevoke)
	require.Equal(t, []string{"admin"}, repo.users[admin])

	names, err := svc.RevokeUserRole(context.Background(), uuid.New(), admin, domain.ProtectedRole)
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestMapError(t *testing.T) {
	t.Parallel()

	cases := map[error]int{
		domain.ErrValidation:         400,
		domain.ErrRoleNotFound:       404,
		domain.ErrUserNotFound:       404,
		domain.ErrPermissionNotFound: 422,
		domain.ErrRoleExists:         409,
		domain.ErrRoleProtected:      403,
		domain.ErrSelfRevoke:         403,
		fmt.Errorf("boom"):           500,
	}

	for err, want := range cases {
		_, _, status := service.MapError(err)
		require.Equal(t, want, status, err.Error())
	}
}
