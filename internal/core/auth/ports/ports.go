// Package ports declares the interfaces the auth service depends on and exposes.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// UserRepository is the user storage the auth service needs.
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (userdomain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error)
	RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	Create(ctx context.Context, user userdomain.User) (userdomain.User, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string, changedAt time.Time) error
	// UpdateEmail sets a verified address; a live duplicate is authdomain.ErrEmailTaken.
	UpdateEmail(ctx context.Context, id uuid.UUID, email string, verifiedAt time.Time) error
}

// SessionRepository stores refresh sessions.
type SessionRepository interface {
	Create(ctx context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error)
	FindByTokenHash(ctx context.Context, hash string) (authdomain.RefreshSession, error)
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error
	RevokeFamily(ctx context.Context, userID, familyID uuid.UUID, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error
	Replace(ctx context.Context, oldID, newID uuid.UUID, at time.Time) error
}

// ResetTokenRepository stores password reset tokens.
type ResetTokenRepository interface {
	Create(ctx context.Context, token authdomain.PasswordResetToken) error
	FindByHash(ctx context.Context, hash string) (authdomain.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error
	// RevokePending marks the user's unused tokens of purpose as used.
	RevokePending(ctx context.Context, userID uuid.UUID, purpose string, at time.Time) error
}

// EmailChangeNotifier delivers account emails. Implementations must not fail
// the request; delivery problems are theirs to log or retry.
type EmailChangeNotifier interface {
	// EmailChangeRequested sends the confirmation token to the new address and a notice to the current one.
	EmailChangeRequested(ctx context.Context, user userdomain.User, newEmail, rawToken string, expiresAt time.Time)
	// EmailChanged tells both addresses that the change completed.
	EmailChanged(ctx context.Context, user userdomain.User, oldEmail, newEmail string)
	// PasswordReset sends the reset token to the account address.
	PasswordReset(ctx context.Context, user userdomain.User, rawToken string, expiresAt time.Time)
	// PasswordChanged tells the account that its password was updated.
	PasswordChanged(ctx context.Context, user userdomain.User)
}

// EmailChanger is the email change use-case API consumed by HTTP handlers.
type EmailChanger interface {
	RequestEmailChange(ctx context.Context, userID uuid.UUID, newEmail, password string) (authdomain.EmailChangeRequest, error)
	ConfirmEmailChange(ctx context.Context, userID uuid.UUID, rawToken string) (userdomain.User, error)
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) bool
}

// TokenService issues and verifies access, refresh, and reset tokens.
type TokenService interface {
	IssueAccess(claims authdomain.AccessClaims) (string, error)
	ParseAccess(token string) (authdomain.AccessClaims, error)
	IssueRefresh() (raw, hash string, err error)
	HashRefresh(raw string) string
	IssueResetToken() (raw, hash, jti string, err error)
	HashResetToken(raw string) string
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// LoginAttempts tracks failed logins per account key and locks the key after
// too many failures. Implementations decide the thresholds.
type LoginAttempts interface {
	// Locked returns how long key stays locked; zero when it is not locked.
	Locked(ctx context.Context, key string) (time.Duration, error)
	// Fail records a failed attempt and returns the lock duration when this failure locked key.
	Fail(ctx context.Context, key string) (time.Duration, error)
	// Reset clears the failure count after a successful login.
	Reset(ctx context.Context, key string) error
}

// ResetTokenSink optionally captures newly issued reset tokens (tests / local tooling only).
type ResetTokenSink interface {
	Capture(rawToken string)
}

// Service is the auth use-case API consumed by HTTP handlers.
type Service interface {
	Login(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.TokenPair, error)
	Refresh(ctx context.Context, refreshToken, userAgent, ip string) (authdomain.TokenPair, error)
	Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error
	ParseAccessToken(token string) (authdomain.AccessClaims, error)
	ForgotPassword(ctx context.Context, emailOrUsername string) error
	CheckResetToken(ctx context.Context, rawToken string) (authdomain.ResetTokenValidity, error)
	ResetPassword(ctx context.Context, rawToken, newPassword, confirmPassword string) error
	ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (changedAt time.Time, sessionsInvalidated bool, err error)
}
