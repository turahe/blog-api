// Package domain holds the post entity, filters, and update inputs.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Post errors.
var (
	ErrNotFound     = errors.New("post not found")
	ErrConflict     = errors.New("conflict")
	ErrStaleVersion = fmt.Errorf("%w: stale version", ErrConflict)
)

// Status is a post publication state.
type Status string

// Post publication states.
const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

// Post is a blog post.
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

// ListFilter pages public post listings.
type ListFilter struct {
	Page         int
	PerPage      int
	CategoryUUID *uuid.UUID
	TagUUID      *uuid.UUID
}

// AdminListFilter selects and pages posts in the admin list.
type AdminListFilter struct {
	Page            int
	PerPage         int
	Status          string
	AuthorUUID      *uuid.UUID
	CategoryUUID    *uuid.UUID
	Query           string
	ScopeAuthorUUID *uuid.UUID
}

// OptionalCategoryID is a tri-state category change: Present=false means omit;
// Present=true applies Value (nil clears).
type OptionalCategoryID struct {
	Present bool
	Value   *uuid.UUID
}

// UpdateInput holds optional post changes; nil fields are left unchanged.
type UpdateInput struct {
	Title        *string
	Slug         *string
	Excerpt      *string
	Content      *string
	CategoryUUID OptionalCategoryID
	Tags         *[]string
}

// ListResult is a page of posts.
type ListResult struct {
	Items   []Post
	Total   int64
	Page    int
	PerPage int
}
