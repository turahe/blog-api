// Package domain holds in-app notifications: the inbox rows behind the web and SSE channels.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a notification does not exist or belongs to another user.
var ErrNotFound = errors.New("notification not found")

// Notification is one inbox entry for a user.
type Notification struct {
	ID       int64
	UUID     uuid.UUID
	UserUUID uuid.UUID
	Type     string
	Title    string
	Body     string
	// Preview is the short SSE line; Title and Body are the web copy.
	Preview string
	// Payload links the entry to its subject, such as post_id, comment_id, and url.
	Payload   map[string]string
	ActorUUID *uuid.UUID
	// DedupeKey makes a notice idempotent per user; empty means no deduplication.
	DedupeKey string
	ReadAt    *time.Time
	CreatedAt time.Time
}

// Read reports whether the user has marked the notification read.
func (n Notification) Read() bool {
	return n.ReadAt != nil
}

// ListFilter selects a page of one user's notifications, newest first.
type ListFilter struct {
	UserUUID   uuid.UUID
	UnreadOnly bool
	Page       int
	PerPage    int
}

// ListResult is one page of notifications with the unread count across all pages.
type ListResult struct {
	Items   []Notification
	Total   int64
	Unread  int64
	Page    int
	PerPage int
}
