// Package domain holds the user entity and account statuses.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by user lookups that match no account.
var ErrNotFound = errors.New("user not found")

// ErrUsernameTaken reports a username already used by a live account.
var ErrUsernameTaken = errors.New("username already in use")

// Status is a user account state.
type Status string

// User account states.
const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDeleted   Status = "deleted"
)

// User is an account.
type User struct {
	ID                int64
	UUID              uuid.UUID
	Email             string
	Username          string
	FullName          string
	PasswordHash      string
	Status            Status
	EmailVerifiedAt   *time.Time
	PasswordChangedAt *time.Time
	LastLoginAt       *time.Time
	LoginCount        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// IsActive reports whether the account may sign in.
func (u User) IsActive() bool {
	return u.Status == StatusActive && u.DeletedAt == nil
}
