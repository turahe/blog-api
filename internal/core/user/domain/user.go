package domain

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDeleted   Status = "deleted"
)

type User struct {
	ID                uuid.UUID
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

func (u User) IsActive() bool {
	return u.Status == StatusActive && u.DeletedAt == nil
}
