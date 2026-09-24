// Package domain holds the nested-set category entity and its errors.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Category errors.
var (
	ErrNotFound = errors.New("category not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("category in use")
)

// Category is a node of the nested-set category tree.
type Category struct {
	ID          int64
	UUID        uuid.UUID
	Name        string
	Slug        string
	Description string
	ParentUUID  *uuid.UUID
	ImageUUID   *uuid.UUID
	Lft         int
	Rgt         int
	Depth       int
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
