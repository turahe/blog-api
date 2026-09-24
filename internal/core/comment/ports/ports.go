// Package ports declares the comment repository interface.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

// Repository stores comments, flags, and upvotes.
type Repository interface {
	// PostIsPublic returns ErrPostNotFound unless the post is published and not deleted.
	PostIsPublic(ctx context.Context, postID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (commentdomain.Comment, error)
	List(ctx context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error)
	Create(ctx context.Context, comment commentdomain.Comment) (commentdomain.Comment, error)
	// Update persists content, status, edit and soft-delete fields.
	Update(ctx context.Context, comment commentdomain.Comment) (commentdomain.Comment, error)
	// AddFlag records a flag once per identity, bumps flag_count, and moves an approved
	// comment to flagged when the count reaches threshold. added is false for duplicates.
	AddFlag(ctx context.Context, flag commentdomain.Flag, threshold int) (added bool, err error)
	// ToggleUpvote adds the voter's upvote, or removes it when present.
	ToggleUpvote(ctx context.Context, commentID, voterID uuid.UUID, at time.Time) (upvoted bool, count int, err error)

	// GetByIDs returns the comments that exist among ids, in no particular order.
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]commentdomain.Comment, error)
	// ApplyModerations persists every change and its log entry in one transaction. A change
	// whose comment is no longer in its From status aborts the whole batch with a
	// *BatchError wrapping ErrInvalidTransition.
	ApplyModerations(ctx context.Context, changes []commentdomain.Moderation) error
	// HardDelete appends entry, then removes the comment, or scrubs its content and author
	// into a deleted placeholder when it has replies (so they are not cascade-deleted).
	HardDelete(ctx context.Context, id uuid.UUID, entry commentdomain.ModerationEntry) (scrubbed bool, err error)
	// ListFlags returns the comment's flags, newest first.
	ListFlags(ctx context.Context, id uuid.UUID) ([]commentdomain.Flag, error)
	// ListModerationLog returns the comment's moderation history, oldest first.
	ListModerationLog(ctx context.Context, id uuid.UUID) ([]commentdomain.ModerationEntry, error)
	// Stats summarises the moderation queue.
	Stats(ctx context.Context) (commentdomain.Stats, error)
}
