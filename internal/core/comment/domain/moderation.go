package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// MaxBulkModerate caps the comment ids in one bulk moderation request.
const MaxBulkModerate = 500

// MaxModerationReasonRunes caps a moderator's reason.
const MaxModerationReasonRunes = 1000

// ErrInvalidTransition means the action is not allowed from the comment's current status,
// including when another moderator changed the status first.
var ErrInvalidTransition = errors.New("invalid moderation transition")

// Action is a moderator decision on a comment.
type Action string

// Moderator actions. HardDelete is only recorded in the log; it is not a status transition.
const (
	ActionApprove    Action = "approve"
	ActionReject     Action = "reject"
	ActionSpam       Action = "spam"
	ActionRestore    Action = "restore"
	ActionHardDelete Action = "hard_delete"
)

// ModerationActions are the actions accepted by moderate and bulk moderate.
var ModerationActions = []Action{ActionApprove, ActionReject, ActionSpam, ActionRestore}

// QueueStatuses await a moderator decision; the admin list defaults to them.
var QueueStatuses = []Status{StatusPending, StatusFlagged}

var allStatuses = []Status{
	StatusPending, StatusApproved, StatusFlagged, StatusSpam, StatusRejected, StatusDeleted,
}

type transition struct {
	from []Status
	to   Status
}

var transitions = map[Action]transition{
	ActionApprove: {from: []Status{StatusPending, StatusFlagged, StatusRejected, StatusSpam}, to: StatusApproved},
	ActionReject:  {from: []Status{StatusPending, StatusFlagged, StatusApproved, StatusSpam}, to: StatusRejected},
	ActionSpam:    {from: []Status{StatusPending, StatusFlagged, StatusApproved, StatusRejected}, to: StatusSpam},
	ActionRestore: {from: []Status{StatusDeleted}, to: StatusApproved},
}

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	return slices.Contains(allStatuses, s)
}

// Transition returns the status that action moves a comment in from to.
func Transition(from Status, action Action) (Status, error) {
	t, ok := transitions[action]
	if !ok {
		return "", fmt.Errorf("%w: unknown action %q", ErrValidation, action)
	}

	if !slices.Contains(t.from, from) {
		return "", fmt.Errorf("%w: cannot %s a %s comment", ErrInvalidTransition, action, from)
	}

	return t.to, nil
}

// Scrubbed reports whether a hard delete blanked this comment into a reply placeholder;
// user input can never be empty, so empty content only comes from scrubbing.
func (c Comment) Scrubbed() bool {
	return c.Status == StatusDeleted && c.Content == ""
}

// BatchError lists the comments that blocked an all-or-nothing bulk moderation.
type BatchError struct {
	Err error
	IDs []uuid.UUID
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("%v: %d comment(s)", e.Err, len(e.IDs))
}

func (e *BatchError) Unwrap() error { return e.Err }

// Snapshot is the comment state recorded before and after a moderation action.
func (c Comment) Snapshot() map[string]any {
	return map[string]any{
		"status":            string(c.Status),
		"content":           c.Content,
		"flag_count":        c.FlagCount,
		"author_name":       c.AuthorName,
		"author_email":      c.AuthorEmail,
		"moderation_reason": c.ModerationReason,
		"deleted":           c.DeletedAt != nil,
	}
}

// ModerationEntry is one append-only moderation log row. CommentUUID survives a hard delete.
type ModerationEntry struct {
	UUID          uuid.UUID
	CommentUUID   uuid.UUID
	ModeratorUUID *uuid.UUID
	Action        Action
	FromStatus    Status
	ToStatus      Status
	Reason        string
	NotifyAuthor  bool
	Before        map[string]any
	After         map[string]any
	CreatedAt     time.Time
}

// Moderation is a decided status change: Comment holds the new state, applied only while
// the stored comment is still in From, together with its log entry.
type Moderation struct {
	Comment Comment
	From    Status
	Entry   ModerationEntry
}

// Review is the moderator's full view of one comment.
type Review struct {
	Comment Comment
	Flags   []Flag
	History []ModerationEntry
}

// PostQueue is one post's share of the moderation queue.
type PostQueue struct {
	PostUUID  uuid.UUID
	PostTitle string
	Pending   int64
	Flagged   int64
}

// Stats summarises moderation health.
type Stats struct {
	ByStatus       map[Status]int64
	QueueDepth     int64
	OldestQueuedAt *time.Time
	TopPosts       []PostQueue
}
