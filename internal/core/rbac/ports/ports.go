// Package ports declares the permission enforcer used by authorization middleware
// and the role store used by user and role administration.
package ports

import (
	"context"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/rbac/domain"
)

// Enforcer decides whether a user holds a permission.
type Enforcer interface {
	Enforce(ctx context.Context, userID uuid.UUID, permission string) (bool, error)
}

// RoleAssigner grants roles to users, keeping user_roles and the enforcer in sync.
type RoleAssigner interface {
	// CheckRoles returns domain.ErrRoleNotFound when any name is not a role.
	CheckRoles(ctx context.Context, names []string) error
	// AssignRoles adds the roles to the user; existing assignments are kept.
	AssignRoles(ctx context.Context, userID uuid.UUID, names []string) error
}

// RoleRepository persists roles, their permission sets, and user assignments.
// Every write also updates the enforcer policy.
type RoleRepository interface {
	RoleAssigner

	ListRoles(ctx context.Context) ([]domain.Role, error)
	// FindRole returns domain.ErrRoleNotFound for an unknown name.
	FindRole(ctx context.Context, name string) (domain.Role, error)
	// CreateRole returns domain.ErrRoleExists when the name is taken.
	CreateRole(ctx context.Context, role domain.Role) (domain.Role, error)
	UpdateRole(ctx context.Context, name, description string) (domain.Role, error)
	// DeleteRole removes the role, its grants, and its user assignments.
	DeleteRole(ctx context.Context, name string) error
	// SetRolePermissions replaces the role's permission set.
	SetRolePermissions(ctx context.Context, name string, keys []string) (domain.Role, error)

	ListPermissions(ctx context.Context) ([]domain.Permission, error)
	// CheckPermissions returns domain.ErrPermissionNotFound when any key is unregistered.
	CheckPermissions(ctx context.Context, keys []string) error

	// UserRoles returns domain.ErrUserNotFound for an unknown user.
	UserRoles(ctx context.Context, userID uuid.UUID) ([]string, error)
	// RevokeRole removes one assignment; revoking an unassigned role is a no-op.
	RevokeRole(ctx context.Context, userID uuid.UUID, name string) error
}
