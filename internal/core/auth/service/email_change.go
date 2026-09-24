package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const maxEmailBytes = 254

// Email change token errors, distinct from the password reset ones so handlers can map them.
var (
	ErrEmailChangeTokenInvalid = errors.New("invalid email change token")
	ErrEmailChangeTokenUsed    = errors.New("email change token already used")
	ErrEmailChangeTokenExpired = errors.New("email change token expired")
)

// WithEmailChange sets the notifier for email change messages and the cache holding
// public profiles that show the address.
func (s *AuthService) WithEmailChange(notifier ports.EmailChangeNotifier, cache readcache.Cache) *AuthService {
	s.notifier = notifier
	s.cache = cache

	return s
}

// RequestEmailChange verifies the password and issues a confirmation token for
// newEmail, superseding any earlier pending request.
func (s *AuthService) RequestEmailChange(ctx context.Context, userID uuid.UUID, newEmail, password string) (authdomain.EmailChangeRequest, error) {
	email, err := normalizeEmail(newEmail)
	if err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	if user.PasswordHash == "" || !s.hasher.Compare(user.PasswordHash, password) {
		return authdomain.EmailChangeRequest{}, authdomain.ErrCurrentPassword
	}

	if strings.EqualFold(user.Email, email) {
		return authdomain.EmailChangeRequest{}, fmt.Errorf("%w: new_email must differ from the current email", authdomain.ErrValidation)
	}

	if err := s.emailAvailable(ctx, email, userID); err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	raw, hash, jti, err := s.tokens.IssueResetToken()
	if err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	now := s.clock.Now()
	if err := s.resets.RevokePending(ctx, userID, authdomain.PurposeEmailChange, now); err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	request := authdomain.EmailChangeRequest{NewEmail: email, ExpiresAt: now.Add(s.cfg.EmailChangeTTL)}

	err = s.resets.Create(ctx, authdomain.PasswordResetToken{
		UUID: s.ids.New(), UserUUID: userID, JTI: jti, TokenHash: hash,
		Purpose: authdomain.PurposeEmailChange, NewEmail: email, ExpiresAt: request.ExpiresAt, CreatedAt: now,
	})
	if err != nil {
		return authdomain.EmailChangeRequest{}, err
	}

	if s.notifier != nil {
		s.notifier.EmailChangeRequested(ctx, user, email, raw, request.ExpiresAt)
	}

	return request, nil
}

// ConfirmEmailChange consumes the caller's token, switches to the verified new
// address, and revokes every session of the user.
func (s *AuthService) ConfirmEmailChange(ctx context.Context, userID uuid.UUID, rawToken string) (userdomain.User, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return userdomain.User{}, fmt.Errorf("%w: token required", authdomain.ErrValidation)
	}

	token, err := s.emailChangeToken(ctx, userID, rawToken)
	if err != nil {
		return userdomain.User{}, err
	}

	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return userdomain.User{}, err
	}

	if err := s.emailAvailable(ctx, token.NewEmail, userID); err != nil {
		return userdomain.User{}, err
	}

	now := s.clock.Now()
	if err := s.users.UpdateEmail(ctx, userID, token.NewEmail, now); err != nil {
		return userdomain.User{}, err
	}

	if err := s.resets.MarkUsed(ctx, token.UUID, now); err != nil {
		return userdomain.User{}, err
	}

	if err := s.sessions.RevokeAllForUser(ctx, userID, now); err != nil {
		return userdomain.User{}, err
	}

	readcache.Invalidate(ctx, s.cache, readcache.Users)

	if s.notifier != nil {
		s.notifier.EmailChanged(ctx, user, user.Email, token.NewEmail)
	}

	return s.users.FindByID(ctx, userID)
}

// emailChangeToken returns a usable email change token that belongs to userID.
func (s *AuthService) emailChangeToken(ctx context.Context, userID uuid.UUID, rawToken string) (authdomain.PasswordResetToken, error) {
	token, err := s.resets.FindByHash(ctx, s.tokens.HashResetToken(rawToken))
	if errors.Is(err, authdomain.ErrInvalidToken) {
		return authdomain.PasswordResetToken{}, ErrEmailChangeTokenInvalid
	}

	if err != nil {
		return authdomain.PasswordResetToken{}, err
	}

	if token.Purpose != authdomain.PurposeEmailChange || token.UserUUID != userID || token.NewEmail == "" {
		return authdomain.PasswordResetToken{}, ErrEmailChangeTokenInvalid
	}

	if token.UsedAt != nil {
		return authdomain.PasswordResetToken{}, ErrEmailChangeTokenUsed
	}

	if !token.ExpiresAt.After(s.clock.Now()) {
		return authdomain.PasswordResetToken{}, ErrEmailChangeTokenExpired
	}

	return token, nil
}

// emailAvailable reports authdomain.ErrEmailTaken when another live account uses email.
func (s *AuthService) emailAvailable(ctx context.Context, email string, userID uuid.UUID) error {
	existing, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, userdomain.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if existing.UUID != userID {
		return authdomain.ErrEmailTaken
	}

	return nil
}

// normalizeEmail trims and lowercases a bare address, rejecting display-name forms.
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > maxEmailBytes || !strings.Contains(email[strings.LastIndex(email, "@")+1:], ".") {
		return "", fmt.Errorf("%w: new_email must be a valid email address", authdomain.ErrValidation)
	}

	return email, nil
}

// MapEmailChangeError maps email change errors to an error code, message, and HTTP status.
func MapEmailChangeError(err error) (code, message string, status int) {
	switch {
	case errors.Is(err, ErrEmailChangeTokenInvalid):
		return "auth.email.change_token_invalid", "Invalid email change token", 400
	case errors.Is(err, ErrEmailChangeTokenUsed):
		return "auth.email.change_token_used", "Email change token already used", 400
	case errors.Is(err, ErrEmailChangeTokenExpired):
		return "auth.email.change_token_expired", "Email change token expired", 400
	default:
		return MapError(err)
	}
}
