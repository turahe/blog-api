package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("post not found")
	ErrConflict     = errors.New("conflict")
	ErrStaleVersion = fmt.Errorf("%w: stale version", ErrConflict)
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

type Post struct {
	ID                  int64
	UUID                uuid.UUID
	AuthorUUID          uuid.UUID
	CategoryUUID        *uuid.UUID
	Title               string
	Slug                string
	Excerpt             string
	Content             string
	CoverImageMediaUUID *uuid.UUID
	Status              Status
	Version             int64
	PublishedAt         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

type ListFilter struct {
	Page         int
	PerPage      int
	CategoryUUID *uuid.UUID
	TagUUID      *uuid.UUID
}

type AdminListFilter struct {
	Page            int
	PerPage         int
	Status          string
	AuthorUUID      *uuid.UUID
	CategoryUUID    *uuid.UUID
	Query           string
	ScopeAuthorUUID *uuid.UUID
}

// Present=false means omit; Present=true applies Value (nil clears).
type OptionalCategoryID struct {
	Present bool
	Value   *uuid.UUID
}

type UpdateInput struct {
	Title        *string
	Slug         *string
	Excerpt      *string
	Content      *string
	CategoryUUID OptionalCategoryID
	Tags         *[]string
}

type ListResult struct {
	Items   []Post
	Total   int64
	Page    int
	PerPage int
}
