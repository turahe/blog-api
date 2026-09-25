// Package service implements role and permission administration.
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/rbac/domain"
	"github.com/turahe/blog-api/internal/core/rbac/ports"
)

const (
	maxDescriptionLen = 255
	maxPermissions    = 200
	maxRolesPerCall   = 20
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,59}$`)

// RoleService manages roles, their permission sets, and user role assignments.
type RoleService struct {
	repo ports.RoleRepository
}

// NewRoleService returns a RoleService over repo.
func NewRoleService(repo ports.RoleRepository) *RoleService {
	return &RoleService{repo: repo}
}

// NewRole is the input for creating a role.
type NewRole struct {
	Name        string
	Description string
	Permissions []string
}

// List returns every role with its permissions.
func (s *RoleService) List(ctx context.Context) ([]domain.Role, error) {
	return s.repo.ListRoles(ctx)
}

// Get returns one role by name.
func (s *RoleService) Get(ctx context.Context, name string) (domain.Role, error) {
	return s.repo.FindRole(ctx, name)
}

// Create adds a custom role with an optional initial permission set.
func (s *RoleService) Create(ctx context.Context, in NewRole) (domain.Role, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)

	if !roleNamePattern.MatchString(in.Name) {
		return domain.Role{}, fmt.Errorf("%w: name must be 2-60 characters of a-z, 0-9 or '_', starting with a letter", domain.ErrValidation)
	}

	if err := validateDescription(in.Description); err != nil {
		return domain.Role{}, err
	}

	keys, err := s.checkPermissions(ctx, in.Permissions)
	if err != nil {
		return domain.Role{}, err
	}

	return s.repo.CreateRole(ctx, domain.Role{Name: in.Name, Description: in.Description, Permissions: keys})
}

// Update changes a role's description.
func (s *RoleService) Update(ctx context.Context, name, description string) (domain.Role, error) {
	description = strings.TrimSpace(description)
	if err := validateDescription(description); err != nil {
		return domain.Role{}, err
	}

	return s.repo.UpdateRole(ctx, name, description)
}

// Delete removes a custom role together with its grants and assignments.
func (s *RoleService) Delete(ctx context.Context, name string) error {
	if name == domain.ProtectedRole {
		return domain.ErrRoleProtected
	}

	return s.repo.DeleteRole(ctx, name)
}

// SetPermissions replaces a custom role's permission set.
func (s *RoleService) SetPermissions(ctx context.Context, name string, keys []string) (domain.Role, error) {
	if name == domain.ProtectedRole {
		return domain.Role{}, domain.ErrRoleProtected
	}

	keys, err := s.checkPermissions(ctx, keys)
	if err != nil {
		return domain.Role{}, err
	}

	return s.repo.SetRolePermissions(ctx, name, keys)
}

// Permissions returns the registered permission catalog.
func (s *RoleService) Permissions(ctx context.Context) ([]domain.Permission, error) {
	return s.repo.ListPermissions(ctx)
}

// UserRoles returns the names of the roles assigned to the user.
func (s *RoleService) UserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.repo.UserRoles(ctx, userID)
}

// AssignUserRoles adds roles to the user and returns the resulting assignment.
func (s *RoleService) AssignUserRoles(ctx context.Context, userID uuid.UUID, names []string) ([]string, error) {
	names = dedupe(names)
	if len(names) == 0 || len(names) > maxRolesPerCall {
		return nil, fmt.Errorf("%w: send 1-%d role names", domain.ErrValidation, maxRolesPerCall)
	}

	before, err := s.repo.UserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}

	before = slices.Clone(before)

	if err := s.repo.AssignRoles(ctx, userID, names); err != nil {
		return nil, err
	}

	return s.recordRoles(ctx, userID, before)
}

// RevokeUserRole removes one role from the user. Administrators cannot revoke
// the protected role from themselves, so an install cannot lose its last admin
// by accident.
func (s *RoleService) RevokeUserRole(ctx context.Context, actor, userID uuid.UUID, name string) ([]string, error) {
	if actor == userID && name == domain.ProtectedRole {
		return nil, domain.ErrSelfRevoke
	}

	if _, err := s.repo.FindRole(ctx, name); err != nil {
		return nil, err
	}

	before, err := s.repo.UserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}

	before = slices.Clone(before)

	if err := s.repo.RevokeRole(ctx, userID, name); err != nil {
		return nil, err
	}

	return s.recordRoles(ctx, userID, before)
}

// recordRoles returns the user's roles after a change and notes it for the audit log.
func (s *RoleService) recordRoles(ctx context.Context, userID uuid.UUID, before []string) ([]string, error) {
	after, err := s.repo.UserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}

	audit.AddChange(ctx, "roles", before, slices.Clone(after))

	return after, nil
}

func (s *RoleService) checkPermissions(ctx context.Context, keys []string) ([]string, error) {
	keys = dedupe(keys)
	if len(keys) > maxPermissions {
		return nil, fmt.Errorf("%w: at most %d permissions", domain.ErrValidation, maxPermissions)
	}

	if slices.Contains(keys, domain.WildcardPermission) {
		return nil, fmt.Errorf("%w: the %q permission is reserved for the %s role", domain.ErrValidation, domain.WildcardPermission, domain.ProtectedRole)
	}

	if err := s.repo.CheckPermissions(ctx, keys); err != nil {
		return nil, err
	}

	return keys, nil
}

func validateDescription(description string) error {
	if utf8.RuneCountInString(description) > maxDescriptionLen {
		return fmt.Errorf("%w: description must be at most %d characters", domain.ErrValidation, maxDescriptionLen)
	}

	return nil
}

func dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out
}

// MapError maps RBAC errors to an error code, message, and HTTP status.
func MapError(err error) (code, message string, status int) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return "validation_error", err.Error(), 400
	case errors.Is(err, domain.ErrRoleNotFound):
		return "rbac.role.not_found", err.Error(), 404
	case errors.Is(err, domain.ErrUserNotFound):
		return "user.not_found", "User not found", 404
	case errors.Is(err, domain.ErrPermissionNotFound):
		return "rbac.permission.not_found", err.Error(), 422
	case errors.Is(err, domain.ErrRoleExists):
		return "rbac.role.exists", "A role with this name already exists", 409
	case errors.Is(err, domain.ErrRoleProtected):
		return "rbac.role.protected", "The admin role cannot be deleted or have its permissions changed", 403
	case errors.Is(err, domain.ErrSelfRevoke):
		return "rbac.role.self_revoke", "You cannot revoke the admin role from yourself", 403
	default:
		return "internal_error", "Internal server error", 500
	}
}
