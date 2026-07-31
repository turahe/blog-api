package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("category not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("category in use")
)

type Category struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
	Lft         int
	Rgt         int
	Depth       int
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
