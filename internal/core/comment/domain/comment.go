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
	// ErrCommentsDisabled means the post's policy hides its comments and accepts none.
	ErrCommentsDisabled = errors.New("comments are disabled on this post")
	// ErrCommentsClosed means the post is read-only: comments are shown but not added,
	// edited, or upvoted.
	ErrCommentsClosed = errors.New("comments are closed on this post")
	// ErrChallengeFailed means a guest's captcha token was missing or rejected.
	ErrChallengeFailed = errors.New("captcha verification failed")
	// ErrChallengeUnavailable means the captcha provider could not be reached.
	ErrChallengeUnavailable = errors.New("captcha verification unavailable")
)

// Policy is a post's comment policy.
type Policy string

// Post comment policies.
const (
	PolicyOpen          Policy = "open"
	PolicyAuthenticated Policy = "authenticated"
	PolicyReadOnly      Policy = "read_only"
	PolicyDisabled      Policy = "disabled"
)

// Readable returns ErrCommentsDisabled when the policy hides comments.
func (p Policy) Readable() error {
	if p == PolicyDisabled {
		return ErrCommentsDisabled
	}

	return nil
}

// Writable returns the error that blocks new comments, edits, and upvotes, or nil.
func (p Policy) Writable() error {
	switch p {
	case PolicyDisabled:
		return ErrCommentsDisabled
	case PolicyReadOnly:
		return ErrCommentsClosed
	case PolicyOpen, PolicyAuthenticated:
	}

	return nil
}

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
	// ContentHTML is Content rendered from markdown and sanitized to the comment allow-list.
	ContentHTML   string
	Status        Status
	Depth         int
	UpvoteCount   int
	FlagCount     int
	ReplyCount    int
	EditedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	DeletedByUUID *uuid.UUID
	// ModeratedByUUID, ModerationReason, and ModeratedAt describe the latest moderator decision.
	ModeratedByUUID  *uuid.UUID
	ModerationReason string
	ModeratedAt      *time.Time
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
