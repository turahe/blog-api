package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

// IssueAccess signs claims as an access token for flows that mint tokens outside login, such
// as impersonation. No refresh session is created.
func (s *AuthService) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	return s.tokens.IssueAccess(claims)
}

// VerifyStepUp reports whether the active user re-proved their identity for a sensitive
// action: their current password, plus a TOTP or backup code when two-factor is enabled.
// A matching backup code is consumed. An enrolled account whose codes cannot be checked
// fails closed.
func (s *AuthService) VerifyStepUp(ctx context.Context, userID uuid.UUID, password, code string) (bool, error) {
	err := s.checkPassword(ctx, userID, password)
	if errors.Is(err, authdomain.ErrCurrentPassword) || errors.Is(err, authdomain.ErrUserInactive) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if s.mfa.repo == nil {
		return true, nil
	}

	_, err = s.enabledEnrollment(ctx, userID)
	if errors.Is(err, authdomain.ErrTwoFactorNotEnrolled) {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	if s.mfa.box == nil || strings.TrimSpace(code) == "" {
		return false, nil
	}

	err = s.verifyCode(ctx, userID, code)
	if errors.Is(err, authdomain.ErrTwoFactorInvalidCode) {
		return false, nil
	}

	return err == nil, err
}
