package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("category not found")
)

type Category struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
