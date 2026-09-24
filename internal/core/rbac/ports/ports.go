// Package ports declares the permission enforcer used by authorization middleware
// and the role store used by user and role administration.
package ports

import (
	"context"

	"github.com/google/uuid"
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
