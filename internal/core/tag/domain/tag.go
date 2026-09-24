// Package domain holds the tag entity and its errors.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Tag errors.
var (
	ErrNotFound = errors.New("tag not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("tag in use")
)

// Tag is a label attached to posts.
type Tag struct {
	ID        int64
	UUID      uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}
