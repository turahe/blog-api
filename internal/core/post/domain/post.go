// Package domain holds the post entity, filters, and update inputs.
package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Post errors.
var (
	ErrNotFound          = errors.New("post not found")
	ErrValidation        = errors.New("validation error")
	ErrConflict          = errors.New("conflict")
	ErrStaleVersion      = fmt.Errorf("%w: stale version", ErrConflict)
	ErrInvalidTransition = fmt.Errorf("%w: invalid status transition", ErrConflict)
)

// MaxSlugLength caps post slugs, including any collision suffix.
const MaxSlugLength = 255

// Status is a post publication state.
type Status string

// Post publication states.
const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

// Transition is a publication status change requested by an editor.
type Transition string

// Publication transitions.
const (
	TransitionPublish   Transition = "publish"
	TransitionUnpublish Transition = "unpublish"
	TransitionArchive   Transition = "archive"
)

var transitions = map[Transition]struct {
	from []Status
	to   Status
}{
	TransitionPublish:   {from: []Status{StatusDraft, StatusScheduled, StatusArchived}, to: StatusPublished},
	TransitionUnpublish: {from: []Status{StatusPublished, StatusScheduled}, to: StatusDraft},
	TransitionArchive:   {from: []Status{StatusDraft, StatusScheduled, StatusPublished}, to: StatusArchived},
}

// Next returns the status that transition moves a post in from to.
func (t Transition) Next(from Status) (Status, error) {
	rule, ok := transitions[t]
	if !ok {
		return "", fmt.Errorf("%w: unknown transition %q", ErrValidation, t)
	}

	if !slices.Contains(rule.from, from) {
		return "", fmt.Errorf("%w: cannot %s a %s post", ErrInvalidTransition, t, from)
	}

	return rule.to, nil
}

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
	// Trashed lists only soft-deleted posts instead of live ones.
	Trashed bool
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
