package service_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/rbac/domain"
	"github.com/turahe/blog-api/internal/core/rbac/service"
)

type memRepo struct {
	roles       map[string]domain.Role
	permissions []string
	users       map[uuid.UUID][]string

	revokeErr error
	// rolesErr fails the rolesErrCall-th UserRoles call (1-based).
	rolesErr      error
	rolesErrCall  int
	userRoleCalls int
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
	m.userRoleCalls++
	if m.rolesErr != nil && m.userRoleCalls == m.rolesErrCall {
		return nil, m.rolesErr
	}

	names, ok := m.users[userID]
	if !ok {
		return nil, domain.ErrUserNotFound
	}

	return names, nil
}

func (m *memRepo) RevokeRole(_ context.Context, userID uuid.UUID, name string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}

	m.users[userID] = slices.DeleteFunc(m.users[userID], func(n string) bool { return n == name })
	return nil
}

func TestListGetAndPermissions(t *testing.T) {
	t.Parallel()

	svc := service.NewRoleService(newMemRepo())

	roles, err := svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, roles, 2)

	role, err := svc.Get(t.Context(), "editor")
	require.NoError(t, err)
	require.Equal(t, []string{"post.read"}, role.Permissions)

	_, err = svc.Get(t.Context(), "ghost")
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	perms, err := svc.Permissions(t.Context())
	require.NoError(t, err)
	require.Equal(t, []domain.Permission{{Key: "post.read"}, {Key: "post.create"}, {Key: "user.read"}}, perms)
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
		"long description":   {service.NewRole{Name: "reviewer", Description: strings.Repeat("d", 256)}, domain.ErrValidation},
		"too many permissions": {service.NewRole{Name: "reviewer", Permissions: func() []string {
			keys := make([]string, 201)
			for i := range keys {
				keys[i] = fmt.Sprintf("perm.%d", i)
			}

			return keys
		}()}, domain.ErrValidation},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := service.NewRoleService(newMemRepo()).Create(context.Background(), tc.in)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestUpdateRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		role        string
		description string
		want        string
		wantErr     error
	}{
		{name: "trims the description", role: "editor", description: "  Edits posts  ", want: "Edits posts"},
		{name: "clears the description", role: "editor", description: "   ", want: ""},
		{name: "too long", role: "editor", description: strings.Repeat("é", 256), wantErr: domain.ErrValidation},
		{name: "unknown role", role: "ghost", description: "x", wantErr: domain.ErrRoleNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newMemRepo()

			role, err := service.NewRoleService(repo).Update(t.Context(), tt.role, tt.description)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, repo.roles["editor"].Description)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, role.Description)
			require.Equal(t, tt.want, repo.roles[tt.role].Description)
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

	_, err = svc.SetPermissions(context.Background(), "editor", []string{"post.nuke"})
	require.ErrorIs(t, err, domain.ErrPermissionNotFound)
}

func TestUserRoles(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	user := uuid.New()
	repo.users[user] = []string{"editor"}
	svc := service.NewRoleService(repo)

	names, err := svc.UserRoles(t.Context(), user)
	require.NoError(t, err)
	require.Equal(t, []string{"editor"}, names)

	_, err = svc.UserRoles(t.Context(), uuid.New())
	require.ErrorIs(t, err, domain.ErrUserNotFound)
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

func TestAssignUserRolesRejectsTooManyNames(t *testing.T) {
	t.Parallel()

	names := make([]string, 21)
	for i := range names {
		names[i] = fmt.Sprintf("role_%d", i)
	}

	_, err := service.NewRoleService(newMemRepo()).AssignUserRoles(t.Context(), uuid.New(), names)
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestUserRoleChangeFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name    string
		breakIt func(*memRepo)
		change  func(svc *service.RoleService, user uuid.UUID) error
		wantErr error
	}{
		{
			name:    "assign cannot read the result",
			breakIt: func(r *memRepo) { r.rolesErr, r.rolesErrCall = boom, 2 },
			change: func(svc *service.RoleService, user uuid.UUID) error {
				_, err := svc.AssignUserRoles(t.Context(), user, []string{"editor"})
				return err
			},
			wantErr: boom,
		},
		{
			name:    "revoke of an unknown role",
			breakIt: func(*memRepo) {},
			change: func(svc *service.RoleService, user uuid.UUID) error {
				_, err := svc.RevokeUserRole(t.Context(), uuid.New(), user, "ghost")
				return err
			},
			wantErr: domain.ErrRoleNotFound,
		},
		{
			name:    "revoke cannot read the current roles",
			breakIt: func(r *memRepo) { r.rolesErr, r.rolesErrCall = boom, 1 },
			change: func(svc *service.RoleService, user uuid.UUID) error {
				_, err := svc.RevokeUserRole(t.Context(), uuid.New(), user, "editor")
				return err
			},
			wantErr: boom,
		},
		{
			name:    "revoke fails",
			breakIt: func(r *memRepo) { r.revokeErr = boom },
			change: func(svc *service.RoleService, user uuid.UUID) error {
				_, err := svc.RevokeUserRole(t.Context(), uuid.New(), user, "editor")
				return err
			},
			wantErr: boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newMemRepo()
			user := uuid.New()
			repo.users[user] = []string{}
			tt.breakIt(repo)

			require.ErrorIs(t, tt.change(service.NewRoleService(repo), user), tt.wantErr)
		})
	}
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
		errors.New("boom"):           500,
	}

	for err, want := range cases {
		_, _, status := service.MapError(err)
		require.Equal(t, want, status, err.Error())
	}
}

func TestUserRoleChangesAreAudited(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := service.NewRoleService(repo)
	user := uuid.New()
	repo.users[user] = []string{}

	ctx, scope := audit.WithScope(context.Background())
	_, err := svc.AssignUserRoles(ctx, user, []string{"editor"})
	require.NoError(t, err)

	var entry auditdomain.Entry
	scope.Apply(&entry)
	require.Equal(t, auditdomain.Change{From: []string{}, To: []string{"editor"}}, entry.Changes["roles"])

	ctx, scope = audit.WithScope(context.Background())
	_, err = svc.RevokeUserRole(ctx, uuid.New(), user, "editor")
	require.NoError(t, err)

	entry = auditdomain.Entry{}
	scope.Apply(&entry)
	require.Equal(t, []string{"editor"}, entry.Changes["roles"].From)
	require.Empty(t, entry.Changes["roles"].To)
}
