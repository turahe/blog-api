// Package domain holds role and permission entities and their errors.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// RBAC errors.
var (
	ErrRoleNotFound       = errors.New("role not found")
	ErrPermissionNotFound = errors.New("permission not found")
	ErrUserNotFound       = errors.New("user not found")
	ErrRoleExists         = errors.New("role already exists")
	ErrRoleProtected      = errors.New("role is protected")
	ErrSelfRevoke         = errors.New("cannot revoke your own protected role")
	ErrValidation         = errors.New("rbac validation error")
)

// ProtectedRole is the built-in administrator role. It cannot be deleted, its
// permission set is fixed, and an administrator cannot revoke it from themselves.
const ProtectedRole = "admin"

// WildcardPermission grants every permission; only the protected role holds it.
const WildcardPermission = "*"

// Role is a named permission set.
type Role struct {
	UUID        uuid.UUID
	Name        string
	Description string
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Permission is a grantable permission key such as "post.publish".
type Permission struct {
	UUID        uuid.UUID
	Key         string
	Description string
}
