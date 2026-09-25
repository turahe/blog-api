package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// WithRoles enables role assignment on administrator-created users.
func (s *AuthService) WithRoles(roles rbacports.RoleAssigner) *AuthService {
	s.roles = roles
	return s
}

// AdminCreateUser creates an active account with a password chosen by an administrator
// and grants the requested roles.
func (s *AuthService) AdminCreateUser(ctx context.Context, in authdomain.NewUser) (userdomain.User, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Username = strings.TrimSpace(in.Username)
	in.FullName = strings.TrimSpace(in.FullName)

	if err := validateNewUser(in); err != nil {
		return userdomain.User{}, err
	}

	if len(in.Roles) > 0 {
		if s.roles == nil {
			return userdomain.User{}, fmt.Errorf("%w: role assignment is unavailable", authdomain.ErrValidation)
		}

		if err := s.roles.CheckRoles(ctx, in.Roles); err != nil {
			return userdomain.User{}, err
		}
	}

	if err := s.ensureIdentityFree(ctx, in.Email, in.Username); err != nil {
		return userdomain.User{}, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return userdomain.User{}, err
	}

	now := s.clock.Now()

	user, err := s.users.Create(ctx, userdomain.User{
		UUID: s.ids.New(), Email: in.Email, Username: in.Username, FullName: in.FullName,
		PasswordHash: hash, Status: userdomain.StatusActive, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return userdomain.User{}, err
	}

	if len(in.Roles) > 0 {
		if err := s.roles.AssignRoles(ctx, user.UUID, in.Roles); err != nil {
			return userdomain.User{}, fmt.Errorf("assign roles: %w", err)
		}
	}

	readcache.Invalidate(ctx, s.cache, readcache.Users)

	return user, nil
}

// AdminResetPassword emails the user a fresh reset link, invalidating earlier
// links, and optionally revokes every session.
func (s *AuthService) AdminResetPassword(ctx context.Context, userID uuid.UUID, revokeSessions bool) (authdomain.AdminReset, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return authdomain.AdminReset{}, err
	}

	if !user.IsActive() {
		return authdomain.AdminReset{}, authdomain.ErrTargetInactive
	}

	now := s.clock.Now()

	expiresAt, err := s.issuePasswordReset(ctx, user)
	if err != nil {
		return authdomain.AdminReset{}, err
	}

	if revokeSessions {
		if err := s.sessions.RevokeAllForUser(ctx, user.UUID, now); err != nil {
			return authdomain.AdminReset{}, err
		}
	}

	return authdomain.AdminReset{ExpiresAt: expiresAt, SessionsRevoked: revokeSessions}, nil
}

func validateNewUser(in authdomain.NewUser) error {
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		return fmt.Errorf("%w: email is invalid", authdomain.ErrValidation)
	}

	if !usernamePattern.MatchString(in.Username) {
		return fmt.Errorf("%w: username must be 3-32 letters, digits, '.', '_' or '-'", authdomain.ErrValidation)
	}

	if in.FullName == "" || len(in.FullName) > 120 {
		return fmt.Errorf("%w: full_name must be 1-120 characters", authdomain.ErrValidation)
	}

	return validatePasswordStrength(in.Password)
}

func (s *AuthService) ensureIdentityFree(ctx context.Context, email, username string) error {
	if _, err := s.users.FindByEmail(ctx, email); err == nil {
		return authdomain.ErrEmailTaken
	} else if !errors.Is(err, userdomain.ErrNotFound) {
		return err
	}

	existing, err := s.users.FindByUsernameOrEmail(ctx, username)
	if err == nil && strings.EqualFold(existing.Username, username) {
		return userdomain.ErrUsernameTaken
	}

	if err != nil && !errors.Is(err, userdomain.ErrNotFound) {
		return err
	}

	return nil
}
