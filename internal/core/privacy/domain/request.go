// Package domain models personal data export and erasure requests.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Privacy request errors.
var (
	ErrNotFound = errors.New("privacy request not found")
	// ErrAlreadyOpen reports that the user already has an open request of that kind.
	ErrAlreadyOpen = errors.New("privacy request already open")
	// ErrReauth reports an erasure request without a valid password proof.
	ErrReauth = errors.New("erasure requires password re-verification")
	// ErrExportUnavailable reports that no object storage is configured for archives.
	ErrExportUnavailable = errors.New("data export storage is not configured")
)

// Kind is what a request does.
type Kind string

// Request kinds.
const (
	KindExport Kind = "export"
	KindErase  Kind = "erase"
)

// Status is where a request is in its lifecycle.
type Status string

// Request statuses.
const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Request is one asynchronous export or erasure of a user's personal data.
type Request struct {
	UUID     uuid.UUID
	UserUUID uuid.UUID
	Kind     Kind
	Status   Status
	// StorageKey locates a completed export archive; it is cleared when the archive is deleted.
	StorageKey *string
	// ExpiresAt is when a completed export archive is deleted.
	ExpiresAt   *time.Time
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// Open reports whether the request is still waiting or being processed.
func (r Request) Open() bool {
	return r.Status == StatusPending || r.Status == StatusRunning
}

// Downloadable reports whether r is a completed export whose archive still exists at now.
func (r Request) Downloadable(now time.Time) bool {
	return r.Kind == KindExport && r.Status == StatusCompleted && r.StorageKey != nil &&
		r.ExpiresAt != nil && now.Before(*r.ExpiresAt)
}
