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
)

// PasswordResetToken is a hashed single-use password reset token.
type PasswordResetToken struct {
	ID        int64
	UUID      uuid.UUID
	UserUUID  uuid.UUID
	JTI       string
	TokenHash string
	Purpose   string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
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
