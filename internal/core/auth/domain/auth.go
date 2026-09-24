// Package domain holds authentication entities: sessions, reset tokens, and access claims.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Authentication errors; handlers map them via service.MapError.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserInactive       = errors.New("user inactive")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrTokenUsed          = errors.New("token already used")
	ErrValidation         = errors.New("validation error")
	ErrPasswordStrength   = errors.New("password strength")
	ErrPasswordMismatch   = errors.New("password confirm mismatch")
	ErrCurrentPassword    = errors.New("current password mismatch")
	ErrEmailTaken         = errors.New("email already in use")
	ErrAccountLocked      = errors.New("account temporarily locked")
	ErrTargetInactive     = errors.New("target account inactive")
)

// LockedError is ErrAccountLocked with the time left until login is allowed again.
type LockedError struct {
	RetryAfter time.Duration
}

func (e LockedError) Error() string        { return ErrAccountLocked.Error() }
func (e LockedError) Is(target error) bool { return target == ErrAccountLocked }

// Single-use token purposes.
const (
	PurposePasswordReset = "password_reset"
	PurposeEmailChange   = "email_change"
)

// PasswordResetToken is a hashed single-use token for a password reset or an email change.
type PasswordResetToken struct {
	ID        int64
	UUID      uuid.UUID
	UserUUID  uuid.UUID
	JTI       string
	TokenHash string
	Purpose   string
	NewEmail  string // pending address, PurposeEmailChange only
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// NewUser is an administrator-created account.
type NewUser struct {
	Email    string
	Username string
	FullName string
	Password string
	Roles    []string
}

// AdminReset is the result of an administrator-initiated password reset.
type AdminReset struct {
	ExpiresAt       time.Time
	SessionsRevoked bool
}

// EmailChangeRequest is a pending email change awaiting confirmation.
type EmailChangeRequest struct {
	NewEmail  string
	ExpiresAt time.Time
}

// Active reports whether the token is unused and unexpired at now.
func (t PasswordResetToken) Active(now time.Time) bool {
	return t.UsedAt == nil && t.ExpiresAt.After(now)
}

// ResetTokenValidity is the result of checking a reset token without consuming it.
type ResetTokenValidity struct {
	Valid     bool
	ExpiresAt time.Time
}

// TokenPair is the access/refresh token response of login and refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int64
}

// RefreshSession is a stored refresh token; rotation links sessions in a family.
type RefreshSession struct {
	ID             int64
	UUID           uuid.UUID
	UserUUID       uuid.UUID
	FamilyID       uuid.UUID
	TokenHash      string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	ReplacedByUUID *uuid.UUID
	UserAgent      string
	IPAddress      string
	CreatedAt      time.Time
}

// Active reports whether the session is unrevoked and unexpired at now.
func (s RefreshSession) Active(now time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(now)
}

// AccessClaims are the verified claims of an access token.
type AccessClaims struct {
	Subject   uuid.UUID
	Email     string
	Username  string
	ExpiresAt time.Time
	IssuedAt  time.Time
	ID        string
}
