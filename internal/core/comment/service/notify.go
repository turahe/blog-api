package service

import (
	"context"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

type noopNotifier struct{}

func (noopNotifier) CommentReplied(context.Context, commentdomain.Comment, commentdomain.Comment) {}

func (noopNotifier) CommentModerated(context.Context, commentdomain.Moderation) {}

// notifyModerations sends notices for applied changes: a moderated notice when the moderator
// asked for one, and a reply notice for every reply that became visible. The notifier
// deduplicates, so re-approving a reply does not notify the parent author twice.
func (s *Service) notifyModerations(ctx context.Context, changes []commentdomain.Moderation) {
	var parentIDs []uuid.UUID

	for _, change := range changes {
		if change.Entry.NotifyAuthor && change.Comment.AuthorUUID != nil {
			s.cfg.Notifier.CommentModerated(ctx, change)
		}

		if becameVisibleReply(change) {
			parentIDs = append(parentIDs, *change.Comment.ParentUUID)
		}
	}

	if len(parentIDs) == 0 {
		return
	}

	parents, err := s.repo.GetByIDs(ctx, uniqueIDs(parentIDs))
	if err != nil {
		return
	}

	byID := make(map[uuid.UUID]commentdomain.Comment, len(parents))
	for _, parent := range parents {
		byID[parent.UUID] = parent
	}

	for _, change := range changes {
		if !becameVisibleReply(change) {
			continue
		}

		if parent, ok := byID[*change.Comment.ParentUUID]; ok {
			s.cfg.Notifier.CommentReplied(ctx, change.Comment, parent)
		}
	}
}

func becameVisibleReply(change commentdomain.Moderation) bool {
	return change.Comment.ParentUUID != nil &&
		change.Entry.ToStatus == commentdomain.StatusApproved &&
		change.From != commentdomain.StatusFlagged
}
