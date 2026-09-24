package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
)

type Config struct {
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	ResetTokenTTL time.Duration
}

type AuthService struct {
	users    ports.UserRepository
	sessions ports.SessionRepository
	resets   ports.ResetTokenRepository
	hasher   ports.PasswordHasher
	tokens   ports.TokenService
	clock    ports.Clock
	ids      ports.IDGenerator
	sink     ports.ResetTokenSink
	cfg      Config
}

func New(
	users ports.UserRepository,
	sessions ports.SessionRepository,
	resets ports.ResetTokenRepository,
	hasher ports.PasswordHasher,
	tokens ports.TokenService,
	clock ports.Clock,
	ids ports.IDGenerator,
	cfg Config,
	sink ports.ResetTokenSink,
) *AuthService {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.ResetTokenTTL <= 0 {
		cfg.ResetTokenTTL = time.Hour
	}
	return &AuthService{
		users: users, sessions: sessions, resets: resets, hasher: hasher,
		tokens: tokens, clock: clock, ids: ids, sink: sink, cfg: cfg,
	}
}

func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.TokenPair, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return authdomain.TokenPair{}, fmt.Errorf("%w: email and password required", authdomain.ErrValidation)
	}

	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return authdomain.TokenPair{}, authdomain.ErrInvalidCredentials
	}
	if !user.IsActive() {
		return authdomain.TokenPair{}, authdomain.ErrUserInactive
	}
	if user.PasswordHash == "" || !s.hasher.Compare(user.PasswordHash, password) {
		return authdomain.TokenPair{}, authdomain.ErrInvalidCredentials
	}

	now := s.clock.Now()
	if err := s.users.RecordLogin(ctx, user.UUID, now); err != nil {
		return authdomain.TokenPair{}, fmt.Errorf("record login: %w", err)
	}

	ttl := s.cfg.RefreshTTL
	if !remember {
		ttl = 7 * 24 * time.Hour
	}
	return s.issuePair(ctx, user.UUID, user.Email, user.Username, userAgent, ip, ttl)
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken, userAgent, ip string) (authdomain.TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}
	hash := s.tokens.HashRefresh(refreshToken)
	session, err := s.sessions.FindByTokenHash(ctx, hash)
	if err != nil {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}
	now := s.clock.Now()
	if session.RevokedAt != nil {
		_ = s.sessions.RevokeFamily(ctx, session.UserUUID, session.FamilyID, now)
		return authdomain.TokenPair{}, authdomain.ErrTokenRevoked
	}
	if !session.ExpiresAt.After(now) {
		return authdomain.TokenPair{}, authdomain.ErrTokenExpired
	}

	user, err := s.users.FindByID(ctx, session.UserUUID)
	if err != nil || !user.IsActive() {
		return authdomain.TokenPair{}, authdomain.ErrUserInactive
	}

	raw, newHash, err := s.tokens.IssueRefresh()
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	newSession := authdomain.RefreshSession{
		UUID:      s.ids.New(),
		UserUUID:  user.UUID,
		FamilyID:  session.FamilyID,
		TokenHash: newHash,
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
		UserAgent: userAgent,
		IPAddress: ip,
		CreatedAt: now,
	}
	if _, err := s.sessions.Create(ctx, newSession); err != nil {
		return authdomain.TokenPair{}, err
	}
	if err := s.sessions.Replace(ctx, session.UUID, newSession.UUID, now); err != nil {
		return authdomain.TokenPair{}, err
	}

	access, err := s.tokens.IssueAccess(authdomain.AccessClaims{
		Subject:   user.UUID,
		Email:     user.Email,
		Username:  user.Username,
		ExpiresAt: now.Add(s.cfg.AccessTTL),
		IssuedAt:  now,
		ID:        s.ids.New().String(),
	})
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	return authdomain.TokenPair{
		AccessToken:  access,
		RefreshToken: raw,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.AccessTTL.Seconds()),
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	hash := s.tokens.HashRefresh(refreshToken)
	session, err := s.sessions.FindByTokenHash(ctx, hash)
	if err != nil {
		return nil
	}
	if session.UserUUID != userID {
		return authdomain.ErrInvalidToken
	}
	return s.sessions.Revoke(ctx, session.UUID, s.clock.Now())
}

func (s *AuthService) ParseAccessToken(token string) (authdomain.AccessClaims, error) {
	return s.tokens.ParseAccess(token)
}

func (s *AuthService) ForgotPassword(ctx context.Context, emailOrUsername string) error {
	emailOrUsername = strings.TrimSpace(emailOrUsername)
	if len(emailOrUsername) < 3 {
		return fmt.Errorf("%w: email_or_username required", authdomain.ErrValidation)
	}
	user, err := s.users.FindByUsernameOrEmail(ctx, emailOrUsername)
	if err != nil || !user.IsActive() {
		return nil
	}
	raw, hash, jti, err := s.tokens.IssueResetToken()
	if err != nil {
		return err
	}
	now := s.clock.Now()
	token := authdomain.PasswordResetToken{
		UUID: s.ids.New(), UserUUID: user.UUID, JTI: jti, TokenHash: hash,
		Purpose: "password_reset", ExpiresAt: now.Add(s.cfg.ResetTokenTTL), CreatedAt: now,
	}
	if err := s.resets.Create(ctx, token); err != nil {
		return err
	}
	if s.sink != nil {
		s.sink.Capture(raw)
	}
	return nil
}

func (s *AuthService) CheckResetToken(ctx context.Context, rawToken string) (authdomain.ResetTokenValidity, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return authdomain.ResetTokenValidity{}, fmt.Errorf("%w: token required", authdomain.ErrValidation)
	}
	token, err := s.resets.FindByHash(ctx, s.tokens.HashResetToken(rawToken))
	if err != nil {
		return authdomain.ResetTokenValidity{Valid: false}, nil
	}
	now := s.clock.Now()
	if token.UsedAt != nil {
		return authdomain.ResetTokenValidity{Valid: false, ExpiresAt: token.ExpiresAt}, nil
	}
	if !token.ExpiresAt.After(now) {
		return authdomain.ResetTokenValidity{Valid: false, ExpiresAt: token.ExpiresAt}, nil
	}
	return authdomain.ResetTokenValidity{Valid: true, ExpiresAt: token.ExpiresAt}, nil
}

func (s *AuthService) ResetPassword(ctx context.Context, rawToken, newPassword, confirmPassword string) error {
	if newPassword != confirmPassword {
		return authdomain.ErrPasswordMismatch
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	token, err := s.resets.FindByHash(ctx, s.tokens.HashResetToken(strings.TrimSpace(rawToken)))
	if err != nil {
		return authdomain.ErrInvalidToken
	}
	now := s.clock.Now()
	if token.UsedAt != nil {
		return authdomain.ErrTokenUsed
	}
	if !token.ExpiresAt.After(now) {
		return authdomain.ErrTokenExpired
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, token.UserUUID, hash, now); err != nil {
		return err
	}
	if err := s.resets.MarkUsed(ctx, token.UUID, now); err != nil {
		return err
	}
	return s.sessions.RevokeAllForUser(ctx, token.UserUUID, now)
}

func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (time.Time, bool, error) {
	if newPassword != confirm {
		return time.Time{}, false, authdomain.ErrPasswordMismatch
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return time.Time{}, false, err
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return time.Time{}, false, err
	}
	if !s.hasher.Compare(user.PasswordHash, current) {
		return time.Time{}, false, authdomain.ErrCurrentPassword
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return time.Time{}, false, err
	}
	now := s.clock.Now()
	if err := s.users.UpdatePassword(ctx, userID, hash, now); err != nil {
		return time.Time{}, false, err
	}
	invalidated := false
	if revokeAll {
		if err := s.sessions.RevokeAllForUser(ctx, userID, now); err != nil {
			return time.Time{}, false, err
		}
		invalidated = true
	}
	return now, invalidated, nil
}

func (s *AuthService) issuePair(
	ctx context.Context,
	userID uuid.UUID,
	email, username, userAgent, ip string,
	refreshTTL time.Duration,
) (authdomain.TokenPair, error) {
	now := s.clock.Now()
	raw, hash, err := s.tokens.IssueRefresh()
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	session := authdomain.RefreshSession{
		UUID:      s.ids.New(),
		UserUUID:  userID,
		FamilyID:  s.ids.New(),
		TokenHash: hash,
		ExpiresAt: now.Add(refreshTTL),
		UserAgent: userAgent,
		IPAddress: ip,
		CreatedAt: now,
	}
	if _, err := s.sessions.Create(ctx, session); err != nil {
		return authdomain.TokenPair{}, err
	}
	access, err := s.tokens.IssueAccess(authdomain.AccessClaims{
		Subject:   userID,
		Email:     email,
		Username:  username,
		ExpiresAt: now.Add(s.cfg.AccessTTL),
		IssuedAt:  now,
		ID:        s.ids.New().String(),
	})
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	return authdomain.TokenPair{
		AccessToken:  access,
		RefreshToken: raw,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.AccessTTL.Seconds()),
	}, nil
}

func validatePasswordStrength(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return authdomain.ErrPasswordStrength
	}
	var upper, lower, digit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !(upper && lower && digit) {
		return authdomain.ErrPasswordStrength
	}
	return nil
}

func MapError(err error) (code string, message string, status int) {
	switch {
	case errors.Is(err, authdomain.ErrValidation):
		return "validation_error", err.Error(), 400
	case errors.Is(err, authdomain.ErrInvalidCredentials):
		return "unauthorized", "Invalid email or password", 401
	case errors.Is(err, authdomain.ErrUserInactive):
		return "unauthorized", "Account is not active", 401
	case errors.Is(err, authdomain.ErrCurrentPassword):
		return "password.current_mismatch", "Current password is incorrect", 403
	case errors.Is(err, authdomain.ErrPasswordMismatch):
		return "password.confirm_mismatch", "Password confirmation does not match", 422
	case errors.Is(err, authdomain.ErrPasswordStrength):
		return "password.strength", "Password does not meet strength requirements", 422
	case errors.Is(err, authdomain.ErrTokenUsed):
		return "auth.password.reset_token_used", "Reset token already used", 400
	case errors.Is(err, authdomain.ErrTokenExpired):
		return "auth.password.reset_token_expired", "Reset token expired", 400
	case errors.Is(err, authdomain.ErrInvalidToken),
		errors.Is(err, authdomain.ErrTokenRevoked):
		return "unauthorized", "Invalid or expired token", 401
	default:
		return "internal_error", "An unexpected error occurred", 500
	}
}

func MapResetError(err error) (code string, message string, status int) {
	switch {
	case errors.Is(err, authdomain.ErrInvalidToken):
		return "auth.password.reset_token_invalid", "Invalid reset token", 400
	default:
		return MapError(err)
	}
}
