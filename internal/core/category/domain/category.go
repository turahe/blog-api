package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound    = errors.New("category not found")
	ErrConflict    = errors.New("conflict")
	ErrHasChildren = errors.New("category has children")
)

type Category struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// OptionalUUID: Present=false means omit; Present=true applies Value (nil clears).
type OptionalUUID struct {
	Present bool
	Value   *uuid.UUID
}

type UpdateInput struct {
	Name        *string
	Slug        *string
	Description *string
	ParentID    OptionalUUID
	ImageID     OptionalUUID
}

type CreateInput struct {
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
	ImageID     *uuid.UUID
}
