package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("post not found")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

type Post struct {
	ID          uuid.UUID
	AuthorID    uuid.UUID
	CategoryID  *uuid.UUID
	Title       string
	Slug        string
	Excerpt     string
	Content     string
	Status      Status
	Version     int64
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type ListFilter struct {
	Page       int
	PerPage    int
	CategoryID *uuid.UUID
	TagID      *uuid.UUID
}

type ListResult struct {
	Items   []Post
	Total   int64
	Page    int
	PerPage int
}
