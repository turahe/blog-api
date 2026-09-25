// Package domain holds audit log entries and activity queries.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Results recorded on an entry.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// ResourceUser is the resource type of entries about a user account; activity
// for a user includes entries where the user is the actor or this resource.
const ResourceUser = "user"

// Change is one field's value before and after a mutation.
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Entry is one audited action.
type Entry struct {
	UUID     uuid.UUID
	Action   string // operation id, for example "admin.posts.publish"
	Category string // user-facing activity category; empty for admin-only actions
	ActorID  *uuid.UUID
	// ImpersonatorID is the staff member who acted as ActorID through impersonation.
	ImpersonatorID *uuid.UUID
	ResourceType   string
	ResourceID     *uuid.UUID
	Result         string
	Status         int
	Changes        map[string]Change
	Metadata       map[string]any
	IP             string
	UserAgent      string
	RequestID      string
	OccurredAt     time.Time
}

// ActivityFilter selects a user's activity: entries the user performed or that
// target the user's account.
type ActivityFilter struct {
	UserID uuid.UUID
	// Categories limits the result; empty means all.
	Categories []string
	// CategorizedOnly drops admin-only entries (those without a category).
	CategorizedOnly bool
	From, To        *time.Time
	Page, PerPage   int
}

// ActivityPage is one page of entries, newest first.
type ActivityPage struct {
	Items   []Entry
	Page    int
	PerPage int
	Total   int64
}
