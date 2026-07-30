package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

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

type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	JTI       string
	TokenHash string
	Purpose   string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

func (t PasswordResetToken) Active(now time.Time) bool {
	return t.UsedAt == nil && t.ExpiresAt.After(now)
}

type ResetTokenValidity struct {
	Valid     bool
	ExpiresAt time.Time
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int64
}

type RefreshSession struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	FamilyID   uuid.UUID
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedBy *uuid.UUID
	UserAgent  string
	IPAddress  string
	CreatedAt  time.Time
}

func (s RefreshSession) Active(now time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(now)
}

type AccessClaims struct {
	Subject   uuid.UUID
	Email     string
	Username  string
	ExpiresAt time.Time
	IssuedAt  time.Time
	ID        string
}
