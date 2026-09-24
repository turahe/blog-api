package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("tag not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("tag in use")
)

type Tag struct {
	ID        int64
	UUID      uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}
