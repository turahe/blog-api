// Package service implements login, token refresh/rotation, logout, and password reset.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/turahe/blog-api/internal/core/audit"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/event"
	rbacdomain "github.com/turahe/blog-api/internal/core/rbac/domain"
	rbacports "github.com/turahe/blog-api/internal/core/rbac/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Config sets token lifetimes.
type Config struct {
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	ResetTokenTTL  time.Duration
	EmailChangeTTL time.Duration
}

// shortSessionTTL is the refresh session lifetime of a login without "remember me".
const shortSessionTTL = 7 * 24 * time.Hour

// deliveryTimeout bounds background delivery of a reset email.
const deliveryTimeout = time.Minute

// AuthService implements ports.Service.
type AuthService struct {
	users    ports.UserRepository
	sessions ports.SessionRepository
	resets   ports.ResetTokenRepository
	hasher   ports.PasswordHasher
	tokens   ports.TokenService
	clock    ports.Clock
	ids      ports.IDGenerator
	sink     ports.ResetTokenSink
	notifier ports.EmailChangeNotifier
	attempts ports.LoginAttempts
	roles    rbacports.RoleAssigner
	access   rbacports.Enforcer
	cache    readcache.Cache
	cfg      Config
	mfa      twoFactorDeps
	oauth    oauthDeps
	events   event.Unit

	dummyOnce sync.Once
	dummyHash string
}

// WithLoginAttempts enables account lockout after repeated failed logins.
// Tracker errors never block a login: lockout fails open like rate limiting.
func (s *AuthService) WithLoginAttempts(attempts ports.LoginAttempts) *AuthService {
	s.attempts = attempts
	return s
}

// WithEvents records account events in the same transaction as each write.
func (s *AuthService) WithEvents(events event.Unit) *AuthService {
	s.events = events
	return s
}

// New returns an AuthService; sink, when non-nil, receives raw reset tokens (dev/test delivery).
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

	if cfg.EmailChangeTTL <= 0 {
		cfg.EmailChangeTTL = time.Hour
	}

	return &AuthService{
		users: users, sessions: sessions, resets: resets, hasher: hasher,
		tokens: tokens, clock: clock, ids: ids, sink: sink, cfg: cfg,
	}
}

// Login verifies credentials and issues a token pair; remember extends the refresh
// lifetime. Accounts with two-factor enabled get a challenge instead of tokens.
func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.LoginResult, error) {
	return s.login(ctx, email, password, userAgent, ip, remember, nil)
}

// AdminAccessPermission is required to sign in at the admin login.
const AdminAccessPermission = "admin.access"

// WithAccessCheck sets the RBAC check used by AdminLogin. Without it AdminLogin
// refuses everyone.
func (s *AuthService) WithAccessCheck(access rbacports.Enforcer) *AuthService {
	s.access = access
	return s
}

// AdminLogin is Login restricted to accounts holding AdminAccessPermission.
// Other accounts get ErrInvalidCredentials, exactly like a wrong password.
func (s *AuthService) AdminLogin(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.LoginResult, error) {
	return s.login(ctx, email, password, userAgent, ip, remember, s.requireAdminAccess)
}

func (s *AuthService) requireAdminAccess(ctx context.Context, user userdomain.User) error {
	if s.access == nil {
		return authdomain.ErrInvalidCredentials
	}

	allowed, err := s.access.Enforce(ctx, user.UUID, AdminAccessPermission)
	if err != nil {
		return fmt.Errorf("check admin access: %w", err)
	}

	if !allowed {
		return authdomain.ErrInvalidCredentials
	}

	return nil
}

// login verifies the password, then gate (when set), then starts two-factor or issues tokens.
func (s *AuthService) login(
	ctx context.Context, email, password, userAgent, ip string, remember bool,
	gate func(context.Context, userdomain.User) error,
) (authdomain.LoginResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := s.authenticate(ctx, email, password)
	if err != nil {
		return authdomain.LoginResult{}, err
	}

	if gate != nil {
		if err := gate(ctx, user); err != nil {
			return authdomain.LoginResult{}, err
		}
	}

	if s.attempts != nil {
		_ = s.attempts.Reset(ctx, loginAttemptKey(email))
	}

	challenge, err := s.startTwoFactor(ctx, user.UUID, authdomain.PendingLogin{
		UserUUID: user.UUID, Remember: remember, UserAgent: userAgent, IPAddress: ip,
	})
	if err != nil || challenge != nil {
		return authdomain.LoginResult{Challenge: challenge}, err
	}

	pair, err := s.completeLogin(ctx, user, userAgent, ip, remember)

	return authdomain.LoginResult{Tokens: pair}, err
}

// authenticate checks the lockout, the account, and the password for a normalized email.
func (s *AuthService) authenticate(ctx context.Context, email, password string) (userdomain.User, error) {
	if email == "" || password == "" {
		return userdomain.User{}, fmt.Errorf("%w: email and password required", authdomain.ErrValidation)
	}

	if err := s.checkLocked(ctx, email); err != nil {
		return userdomain.User{}, err
	}

	user, err := s.users.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, userdomain.ErrNotFound) {
		return userdomain.User{}, err
	}

	// Unknown accounts and accounts without a password still pay for a hash, and the
	// account status is revealed only to a caller who knows the password.
	if user.PasswordHash == "" {
		s.hasher.Compare(s.timingHash(), password)
		return userdomain.User{}, s.loginFailed(ctx, email)
	}

	// Attempts on a real account appear in its activity, whatever the outcome.
	audit.SetActor(ctx, user.UUID)

	if !s.hasher.Compare(user.PasswordHash, password) {
		return userdomain.User{}, s.loginFailed(ctx, email)
	}

	if !user.IsActive() {
		return userdomain.User{}, authdomain.ErrUserInactive
	}

	return user, nil
}

// timingHash is a real hash of a random value, compared against when there is no
// account, so that path costs as much as a wrong password.
func (s *AuthService) timingHash() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = s.hasher.Hash(uuid.NewString())
	})

	return s.dummyHash
}

// completeLogin records the login and issues the session for a fully authenticated user.
func (s *AuthService) completeLogin(ctx context.Context, user userdomain.User, userAgent, ip string, remember bool) (authdomain.TokenPair, error) {
	audit.SetActor(ctx, user.UUID)

	now := s.clock.Now()
	if err := s.users.RecordLogin(ctx, user.UUID, now); err != nil {
		return authdomain.TokenPair{}, fmt.Errorf("record login: %w", err)
	}

	ttl := s.cfg.RefreshTTL
	if !remember {
		ttl = min(shortSessionTTL, s.cfg.RefreshTTL)
	}

	return s.issuePair(ctx, user.UUID, user.Email, user.Username, userAgent, ip, ttl)
}

// loginAttemptKey keys lockout by account identity. Unknown emails are tracked
// too, so a lockout response does not reveal whether an account exists.
func loginAttemptKey(email string) string {
	return "email:" + email
}

func (s *AuthService) checkLocked(ctx context.Context, email string) error {
	if s.attempts == nil {
		return nil
	}

	remaining, err := s.attempts.Locked(ctx, loginAttemptKey(email))
	if err != nil {
		return nil //nolint:nilerr // lockout fails open so a Redis outage cannot block every login
	}

	if remaining <= 0 {
		return nil
	}

	return authdomain.LockedError{RetryAfter: remaining}
}

// loginFailed records the failure and returns the error the caller should see.
func (s *AuthService) loginFailed(ctx context.Context, email string) error {
	if s.attempts == nil {
		return authdomain.ErrInvalidCredentials
	}

	lockedFor, err := s.attempts.Fail(ctx, loginAttemptKey(email))
	if err == nil && lockedFor > 0 {
		return authdomain.LockedError{RetryAfter: lockedFor}
	}

	return authdomain.ErrInvalidCredentials
}

// Refresh rotates the refresh token; reusing a revoked token revokes its whole family.
func (s *AuthService) Refresh(ctx context.Context, refreshToken, userAgent, ip string) (authdomain.TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}

	hash := s.tokens.HashRefresh(refreshToken)

	session, err := s.sessions.FindByTokenHash(ctx, hash)
	if err != nil {
		return authdomain.TokenPair{}, err
	}

	now := s.clock.Now()
	if session.RevokedAt != nil {
		if err := s.sessions.RevokeFamily(ctx, session.UserUUID, session.FamilyID, now); err != nil {
			return authdomain.TokenPair{}, fmt.Errorf("revoke reused token family: %w", err)
		}

		return authdomain.TokenPair{}, authdomain.ErrTokenRevoked
	}

	if !session.ExpiresAt.After(now) {
		return authdomain.TokenPair{}, authdomain.ErrSessionExpired
	}

	user, err := s.activeUser(ctx, session.UserUUID)
	if err != nil {
		return authdomain.TokenPair{}, err
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
		ExpiresAt: now.Add(sessionLifetime(session, s.cfg.RefreshTTL)),
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
		FamilyID:  session.FamilyID,
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

// sessionLifetime is the lifetime session was issued with, so rotation keeps a
// short (non-remember) session short. It never exceeds refreshTTL.
func sessionLifetime(session authdomain.RefreshSession, refreshTTL time.Duration) time.Duration {
	lifetime := session.ExpiresAt.Sub(session.CreatedAt)
	if lifetime <= 0 || lifetime > refreshTTL {
		return refreshTTL
	}

	return lifetime
}

// Logout revokes the user's refresh session; unknown tokens are ignored.
func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}

	hash := s.tokens.HashRefresh(refreshToken)

	session, err := s.sessions.FindByTokenHash(ctx, hash)
	if errors.Is(err, authdomain.ErrInvalidToken) {
		return nil
	}

	if err != nil {
		return err
	}

	if session.UserUUID != userID {
		return authdomain.ErrInvalidToken
	}

	return s.sessions.Revoke(ctx, session.UUID, s.clock.Now())
}

// ParseAccessToken verifies an access token and returns its claims.
func (s *AuthService) ParseAccessToken(token string) (authdomain.AccessClaims, error) {
	return s.tokens.ParseAccess(token)
}

// ForgotPassword issues a reset token for an active account; unknown accounts succeed silently.
func (s *AuthService) ForgotPassword(ctx context.Context, emailOrUsername string) error {
	emailOrUsername = strings.TrimSpace(emailOrUsername)
	if len(emailOrUsername) < 3 {
		return fmt.Errorf("%w: email_or_username required", authdomain.ErrValidation)
	}

	user, err := s.users.FindByUsernameOrEmail(ctx, emailOrUsername)
	if errors.Is(err, userdomain.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if !user.IsActive() {
		return nil
	}

	_, err = s.issuePasswordReset(ctx, user)

	return err
}

// issuePasswordReset replaces any pending reset token for user with a new one and delivers it.
func (s *AuthService) issuePasswordReset(ctx context.Context, user userdomain.User) (time.Time, error) {
	raw, hash, jti, err := s.tokens.IssueResetToken()
	if err != nil {
		return time.Time{}, err
	}

	now := s.clock.Now()
	token := authdomain.PasswordResetToken{
		UUID: s.ids.New(), UserUUID: user.UUID, JTI: jti, TokenHash: hash,
		Purpose: authdomain.PurposePasswordReset, ExpiresAt: now.Add(s.cfg.ResetTokenTTL), CreatedAt: now,
	}

	err = s.events.InTx(ctx, func(ctx context.Context) error {
		if err := s.resets.RevokePending(ctx, user.UUID, authdomain.PurposePasswordReset, now); err != nil {
			return err
		}

		if err := s.resets.Create(ctx, token); err != nil {
			return err
		}

		return s.events.Record(ctx, passwordResetRequestedEvent(user, token))
	})
	if err != nil {
		return time.Time{}, err
	}

	if s.sink != nil {
		s.sink.Capture(raw)
	}

	// Delivered in the background: a slow mail server must not reveal, through
	// response time, that the account exists.
	if s.notifier != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryTimeout)
			defer cancel()

			s.notifier.PasswordReset(ctx, user, raw, token.ExpiresAt)
		}()
	}

	return token.ExpiresAt, nil
}

// CheckResetToken reports whether a reset token can still be used.
func (s *AuthService) CheckResetToken(ctx context.Context, rawToken string) (authdomain.ResetTokenValidity, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return authdomain.ResetTokenValidity{}, fmt.Errorf("%w: token required", authdomain.ErrValidation)
	}

	token, err := s.passwordResetToken(ctx, rawToken)
	if errors.Is(err, authdomain.ErrInvalidToken) {
		return authdomain.ResetTokenValidity{Valid: false}, nil
	}

	if err != nil {
		return authdomain.ResetTokenValidity{}, err
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

// ResetPassword consumes a reset token, sets the new password, and revokes all sessions.
func (s *AuthService) ResetPassword(ctx context.Context, rawToken, newPassword, confirmPassword string) error {
	if newPassword != confirmPassword {
		return authdomain.ErrPasswordMismatch
	}

	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	token, err := s.passwordResetToken(ctx, strings.TrimSpace(rawToken))
	if err != nil {
		return err
	}

	audit.SetActor(ctx, token.UserUUID)

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

	// Spends this token and any other pending reset token for the account.
	if err := s.resets.RevokePending(ctx, token.UserUUID, authdomain.PurposePasswordReset, now); err != nil {
		return err
	}

	return s.sessions.RevokeAllForUser(ctx, token.UserUUID, now)
}

// passwordResetToken finds a token by its raw value, rejecting tokens issued for other purposes.
func (s *AuthService) passwordResetToken(ctx context.Context, rawToken string) (authdomain.PasswordResetToken, error) {
	token, err := s.resets.FindByHash(ctx, s.tokens.HashResetToken(rawToken))
	if err != nil {
		return authdomain.PasswordResetToken{}, err
	}

	if token.Purpose != authdomain.PurposePasswordReset {
		return authdomain.PasswordResetToken{}, authdomain.ErrInvalidToken
	}

	return token, nil
}

// ChangePassword verifies the current password, sets a new one, and revokes
// every session of the user when revokeAll is true.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (time.Time, bool, error) {
	if newPassword != confirm {
		return time.Time{}, false, authdomain.ErrPasswordMismatch
	}

	if err := validatePasswordStrength(newPassword); err != nil {
		return time.Time{}, false, err
	}

	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, userdomain.ErrNotFound) {
		return time.Time{}, false, authdomain.ErrUserInactive
	}

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

	if s.notifier != nil {
		s.notifier.PasswordChanged(ctx, user)
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
		FamilyID:  session.FamilyID,
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

// VerifyPassword reports whether password is the active user's current password. Accounts
// without a password (OAuth sign-in only) never match.
func (s *AuthService) VerifyPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error) {
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return false, err
	}

	return user.PasswordHash != "" && s.hasher.Compare(user.PasswordHash, password), nil
}

// activeUser loads the user, reporting a missing or inactive account as ErrUserInactive.
func (s *AuthService) activeUser(ctx context.Context, id uuid.UUID) (userdomain.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if errors.Is(err, userdomain.ErrNotFound) {
		return userdomain.User{}, authdomain.ErrUserInactive
	}

	if err != nil {
		return userdomain.User{}, err
	}

	if !user.IsActive() {
		return userdomain.User{}, authdomain.ErrUserInactive
	}

	return user, nil
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

	if !upper || !lower || !digit {
		return authdomain.ErrPasswordStrength
	}

	return nil
}

const codeUnauthorized = "unauthorized"

// errorMapping is one MapError row; an empty message means err.Error().
type errorMapping struct {
	err     error
	code    string
	message string
	status  int
}

// errorMappings is checked in order; the first errors.Is match wins.
var errorMappings = []errorMapping{
	{authdomain.ErrValidation, "validation_error", "", 400},
	{authdomain.ErrInvalidCredentials, codeUnauthorized, "Invalid email or password", 401},
	{authdomain.ErrUserInactive, codeUnauthorized, "Account is not active", 401},
	{authdomain.ErrAccountLocked, "auth.login.locked", "Too many failed login attempts, try again later", 429},
	{authdomain.ErrCurrentPassword, "password.current_mismatch", "Current password is incorrect", 403},
	{authdomain.ErrPasswordMismatch, "password.confirm_mismatch", "Password confirmation does not match", 422},
	{authdomain.ErrEmailTaken, "auth.email.taken", "Email address is already in use", 409},
	{userdomain.ErrUsernameTaken, "user.username.taken", "Username is already in use", 409},
	{userdomain.ErrNotFound, "user.not_found", "User not found", 404},
	{authdomain.ErrTargetInactive, "user.inactive", "User account is not active", 409},
	{authdomain.ErrTwoFactorInvalidCode, "auth.2fa.invalid_code", "Invalid or already used code", 401},
	{authdomain.ErrChallengeInvalid, "auth.2fa.challenge_invalid", "The login challenge is invalid or expired; sign in again", 401},
	{authdomain.ErrTwoFactorAlreadyEnabled, "auth.2fa.already_enabled", "Two-factor authentication is already enabled", 409},
	{authdomain.ErrTwoFactorNotEnrolled, "auth.2fa.not_enabled", "Two-factor authentication is not enabled", 409},
	{authdomain.ErrTwoFactorPending, "auth.2fa.not_started", "Start two-factor setup first", 409},
	{authdomain.ErrTwoFactorUnavailable, "auth.2fa.unavailable", "Two-factor authentication is not configured on this server", 503},
	{authdomain.ErrOAuthProviderUnknown, "auth.oauth.provider_unknown", "OAuth provider is not configured", 404},
	{authdomain.ErrOAuthRedirectURI, "auth.oauth.redirect_uri", "redirect_uri is not allowed", 400},
	{authdomain.ErrOAuthStateInvalid, "auth.oauth.state_invalid", "The sign-in request is invalid or expired; start again", 401},
	{authdomain.ErrOAuthExchange, "auth.oauth.exchange_failed", "The provider did not accept the sign-in", 401},
	{authdomain.ErrOAuthNoAccount, "auth.oauth.no_account", "No account is linked to this sign-in", 401},
	{authdomain.ErrOAuthLinkConflict, "auth.oauth.link_conflict", "The account is already linked to another identity at this provider", 409},
	{rbacdomain.ErrRoleNotFound, "rbac.role.not_found", "", 422},
	{authdomain.ErrPasswordStrength, "password.strength", "Password does not meet strength requirements", 422},
	{authdomain.ErrTokenUsed, "auth.password.reset_token_used", "Reset token already used", 400},
	{authdomain.ErrTokenExpired, "auth.password.reset_token_expired", "Reset token expired", 400},
	{authdomain.ErrInvalidToken, codeUnauthorized, "Invalid or expired token", 401},
	{authdomain.ErrTokenRevoked, codeUnauthorized, "Invalid or expired token", 401},
	{authdomain.ErrSessionExpired, codeUnauthorized, "Invalid or expired token", 401},
}

// MapError maps auth errors to an error code, message, and HTTP status.
func MapError(err error) (code, message string, status int) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			if m.message == "" {
				return m.code, err.Error(), m.status
			}

			return m.code, m.message, m.status
		}
	}

	return "internal_error", "An unexpected error occurred", 500
}

// MapResetError maps password reset errors to an error code, message, and HTTP status.
func MapResetError(err error) (code, message string, status int) {
	switch {
	case errors.Is(err, authdomain.ErrInvalidToken):
		return "auth.password.reset_token_invalid", "Invalid reset token", 400
	default:
		return MapError(err)
	}
}
