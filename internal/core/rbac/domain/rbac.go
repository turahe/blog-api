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
	ErrRoleExists         = errors.New("role already exists")
	ErrRoleProtected      = errors.New("role is protected")
	ErrValidation         = errors.New("rbac validation error")
)

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
