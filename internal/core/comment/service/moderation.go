package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

// AdminListInput filters the moderation list; empty Statuses means the queue (pending, flagged).
type AdminListInput struct {
	PostUUID    *uuid.UUID
	Statuses    []commentdomain.Status
	NewestFirst bool
	Page        int
	PerPage     int
}

// AdminList lists comments in any status for moderators, oldest first unless NewestFirst.
func (s *Service) AdminList(ctx context.Context, in AdminListInput) (commentdomain.ListResult, error) {
	statuses := in.Statuses
	if len(statuses) == 0 {
		statuses = commentdomain.QueueStatuses
	}

	for _, status := range statuses {
		if !status.Valid() {
			return commentdomain.ListResult{}, fmt.Errorf("%w: unknown status %q", commentdomain.ErrValidation, status)
		}
	}

	page, perPage := normalizePage(in.Page, in.PerPage)

	return s.repo.List(ctx, commentdomain.ListFilter{
		PostUUID:    in.PostUUID,
		Statuses:    statuses,
		NewestFirst: in.NewestFirst,
		Page:        page,
		PerPage:     perPage,
	})
}

// AdminGet returns a comment in any status with its flags and moderation history.
func (s *Service) AdminGet(ctx context.Context, id uuid.UUID) (commentdomain.Review, error) {
	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return commentdomain.Review{}, err
	}

	flags, err := s.repo.ListFlags(ctx, id)
	if err != nil {
		return commentdomain.Review{}, err
	}

	history, err := s.repo.ListModerationLog(ctx, id)
	if err != nil {
		return commentdomain.Review{}, err
	}

	return commentdomain.Review{Comment: comment, Flags: flags, History: history}, nil
}

// ModerateInput is one moderator decision. NotifyAuthor is recorded for the notification hooks.
type ModerateInput struct {
	ModeratorUUID uuid.UUID
	CommentUUID   uuid.UUID
	Action        commentdomain.Action
	Reason        string
	NotifyAuthor  bool
}

// Moderate applies one action and returns the updated comment.
func (s *Service) Moderate(ctx context.Context, in ModerateInput) (commentdomain.Comment, error) {
	if err := validateAction(in.Action); err != nil {
		return commentdomain.Comment{}, err
	}

	reason, err := normalizeReason(in.Reason)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	comment, err := s.repo.GetByID(ctx, in.CommentUUID)
	if err != nil {
		return commentdomain.Comment{}, err
	}

	change, err := s.decide(comment, in.Action, in.ModeratorUUID, reason, in.NotifyAuthor, s.clock.Now())
	if err != nil {
		return commentdomain.Comment{}, err
	}

	if err := s.repo.ApplyModerations(ctx, []commentdomain.Moderation{change}); err != nil {
		return commentdomain.Comment{}, err
	}

	audit.AddChange(ctx, "status", change.From, change.Entry.ToStatus)
	s.notifyModerations(ctx, []commentdomain.Moderation{change})

	return s.repo.GetByID(ctx, in.CommentUUID)
}

// BulkModerateInput applies one action to many comments.
type BulkModerateInput struct {
	ModeratorUUID uuid.UUID
	CommentUUIDs  []uuid.UUID
	Action        commentdomain.Action
	Reason        string
}

// BulkModerate applies the action to every comment or to none. Unknown ids yield a
// *BatchError wrapping ErrNotFound; disallowed transitions one wrapping ErrInvalidTransition.
// It returns the number of comments changed.
func (s *Service) BulkModerate(ctx context.Context, in BulkModerateInput) (int, error) {
	if err := validateAction(in.Action); err != nil {
		return 0, err
	}

	reason, err := normalizeReason(in.Reason)
	if err != nil {
		return 0, err
	}

	ids := uniqueIDs(in.CommentUUIDs)
	if len(ids) == 0 || len(ids) > commentdomain.MaxBulkModerate {
		return 0, fmt.Errorf("%w: ids must contain 1 to %d comments", commentdomain.ErrValidation, commentdomain.MaxBulkModerate)
	}

	comments, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return 0, err
	}

	in.Reason = reason

	changes, err := s.decideBatch(ids, comments, in)
	if err != nil {
		return 0, err
	}

	if err := s.repo.ApplyModerations(ctx, changes); err != nil {
		return 0, err
	}

	s.notifyModerations(ctx, changes)

	return len(changes), nil
}

// decideBatch decides every id or fails with a *BatchError listing the unknown ids first,
// then the ids whose transition is not allowed.
func (s *Service) decideBatch(ids []uuid.UUID, comments []commentdomain.Comment, in BulkModerateInput) ([]commentdomain.Moderation, error) {
	byID := make(map[uuid.UUID]commentdomain.Comment, len(comments))
	for _, comment := range comments {
		byID[comment.UUID] = comment
	}

	var missing, invalid []uuid.UUID

	changes := make([]commentdomain.Moderation, 0, len(ids))
	now := s.clock.Now()

	for _, id := range ids {
		comment, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}

		change, err := s.decide(comment, in.Action, in.ModeratorUUID, in.Reason, false, now)
		if isTransitionError(err) {
			invalid = append(invalid, id)
			continue
		}

		if err != nil {
			return nil, err
		}

		changes = append(changes, change)
	}

	if len(missing) > 0 {
		return nil, &commentdomain.BatchError{Err: commentdomain.ErrNotFound, IDs: missing}
	}

	if len(invalid) > 0 {
		return nil, &commentdomain.BatchError{Err: commentdomain.ErrInvalidTransition, IDs: invalid}
	}

	return changes, nil
}

// HardDelete permanently removes a comment, or scrubs it into a placeholder when it has
// replies. It reports whether the comment was scrubbed rather than removed.
func (s *Service) HardDelete(ctx context.Context, moderatorID, id uuid.UUID, reason string) (bool, error) {
	reason, err := normalizeReason(reason)
	if err != nil {
		return false, err
	}

	comment, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return false, err
	}

	return s.repo.HardDelete(ctx, id, commentdomain.ModerationEntry{
		UUID:          s.ids.New(),
		CommentUUID:   comment.UUID,
		ModeratorUUID: &moderatorID,
		Action:        commentdomain.ActionHardDelete,
		FromStatus:    comment.Status,
		ToStatus:      commentdomain.StatusDeleted,
		Reason:        reason,
		Before:        comment.Snapshot(),
		CreatedAt:     s.clock.Now(),
	})
}

// Stats summarises the moderation queue.
func (s *Service) Stats(ctx context.Context) (commentdomain.Stats, error) {
	return s.repo.Stats(ctx)
}

// decide validates the transition and builds the new comment state and its log entry.
// Approving a flagged comment resets its flag count (flag rows stay as history), so it
// needs a full threshold of new flags to be escalated again.
func (s *Service) decide(
	comment commentdomain.Comment,
	action commentdomain.Action,
	moderatorID uuid.UUID,
	reason string,
	notifyAuthor bool,
	now time.Time,
) (commentdomain.Moderation, error) {
	to, err := commentdomain.Transition(comment.Status, action)
	if err != nil {
		return commentdomain.Moderation{}, err
	}

	if comment.Scrubbed() {
		return commentdomain.Moderation{}, fmt.Errorf("%w: comment content was permanently deleted", commentdomain.ErrInvalidTransition)
	}

	updated := comment
	updated.Status = to
	updated.ModeratedByUUID = &moderatorID
	updated.ModerationReason = reason
	updated.ModeratedAt = &now
	updated.UpdatedAt = now

	if comment.Status == commentdomain.StatusFlagged && action == commentdomain.ActionApprove {
		updated.FlagCount = 0
	}

	if action == commentdomain.ActionRestore {
		updated.DeletedAt = nil
		updated.DeletedByUUID = nil
	}

	return commentdomain.Moderation{
		Comment: updated,
		From:    comment.Status,
		Entry: commentdomain.ModerationEntry{
			UUID:          s.ids.New(),
			CommentUUID:   comment.UUID,
			ModeratorUUID: &moderatorID,
			Action:        action,
			FromStatus:    comment.Status,
			ToStatus:      to,
			Reason:        reason,
			NotifyAuthor:  notifyAuthor,
			Before:        comment.Snapshot(),
			After:         updated.Snapshot(),
			CreatedAt:     now,
		},
	}, nil
}

func validateAction(action commentdomain.Action) error {
	if !slices.Contains(commentdomain.ModerationActions, action) {
		return fmt.Errorf("%w: unknown action %q", commentdomain.ErrValidation, action)
	}

	return nil
}

func isTransitionError(err error) bool {
	return errors.Is(err, commentdomain.ErrInvalidTransition)
}

func normalizeReason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if utf8.RuneCountInString(reason) > commentdomain.MaxModerationReasonRunes {
		return "", fmt.Errorf("%w: reason too long", commentdomain.ErrValidation)
	}

	return reason, nil
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}

		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}
