// Package domain holds the comment entity, its moderation states, and flags.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// MaxDepth is the deepest reply level; roots are depth 0. The database enforces the same bound.
const MaxDepth = 5

// MaxContentRunes caps comment content length.
const MaxContentRunes = 10000

// MaxFlagDetailsRunes caps the free-text part of a flag.
const MaxFlagDetailsRunes = 2000

// Comment errors; handlers map them to HTTP responses.
var (
	ErrNotFound         = errors.New("comment not found")
	ErrPostNotFound     = errors.New("post not found")
	ErrValidation       = errors.New("validation error")
	ErrForbidden        = errors.New("not the comment author")
	ErrEditWindowClosed = errors.New("edit window closed")
	ErrNotEditable      = errors.New("comment is not editable")
	ErrDepthExceeded    = errors.New("reply depth exceeded")
	ErrParentInvalid    = errors.New("invalid parent comment")
	ErrGuestDisabled    = errors.New("guest comments are disabled")
)

// Status is a comment moderation state.
type Status string

// Comment moderation states.
const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusFlagged  Status = "flagged"
	StatusSpam     Status = "spam"
	StatusRejected Status = "rejected"
	StatusDeleted  Status = "deleted"
)

// PublicStatuses are shown to readers; deleted comments render as placeholders.
var PublicStatuses = []Status{StatusApproved, StatusDeleted}

// FlagReasons are the accepted flag reason codes.
var FlagReasons = []string{
	"spam", "abuse", "hate", "harassment", "doxx", "self_harm",
	"copyright", "impersonation", "illegal", "other",
}

// Comment is a threaded comment on a post, by a user or a guest.
type Comment struct {
	ID             int64
	UUID           uuid.UUID
	PostUUID       uuid.UUID
	ParentUUID     *uuid.UUID
	AuthorUUID     *uuid.UUID
	AuthorUsername string
	AuthorName     string
	AuthorEmail    string
	IPHash         string
	UserAgent      string
	Content        string
	Status         Status
	Depth          int
	UpvoteCount    int
	FlagCount      int
	ReplyCount     int
	EditedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	DeletedByUUID  *uuid.UUID
}

// Public reports whether readers may see the comment (content hidden when deleted).
func (c Comment) Public() bool {
	return c.Status == StatusApproved || c.Status == StatusDeleted
}

// OwnedBy reports whether userID authored the comment.
func (c Comment) OwnedBy(userID uuid.UUID) bool {
	return c.AuthorUUID != nil && *c.AuthorUUID == userID
}

// Flag is a report that a comment breaks the rules.
type Flag struct {
	CommentUUID    uuid.UUID
	ReporterUUID   *uuid.UUID
	ReporterIPHash string
	Reason         string
	Details        string
	CreatedAt      time.Time
}

// ListFilter selects and pages comments.
type ListFilter struct {
	PostUUID    *uuid.UUID
	ParentUUID  *uuid.UUID
	RootsOnly   bool
	AuthorUUID  *uuid.UUID
	Statuses    []Status
	NewestFirst bool
	Page        int
	PerPage     int
}

// ListResult is a page of comments.
type ListResult struct {
	Items   []Comment
	Total   int64
	Page    int
	PerPage int
}

// Thread is a comment with the first page of its direct replies.
type Thread struct {
	Comment Comment
	Replies ListResult
}
