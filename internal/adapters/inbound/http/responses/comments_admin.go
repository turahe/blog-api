package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

// AdminComment serializes a comment for moderators: unlike Comment it keeps the content and
// author of deleted comments and adds the identity and moderation fields.
func AdminComment(comment commentdomain.Comment) gin.H {
	payload := Comment(comment)
	payload["content"] = comment.Content
	payload["content_html"] = comment.ContentHTML
	payload["author"] = commentAuthor(comment)
	payload["author_email"] = emptyToNil(comment.AuthorEmail)
	payload["ip_hash"] = emptyToNil(comment.IPHash)
	payload["user_agent"] = emptyToNil(comment.UserAgent)
	payload["flag_count"] = comment.FlagCount
	payload["moderated_by"] = uuidString(comment.ModeratedByUUID)
	payload["moderation_reason"] = emptyToNil(comment.ModerationReason)
	payload["moderated_at"] = RFC3339(comment.ModeratedAt)
	payload["deleted_at"] = RFC3339(comment.DeletedAt)
	payload["deleted_by"] = uuidString(comment.DeletedByUUID)

	return payload
}

// CommentReview serializes a comment with its flags and moderation history.
func CommentReview(review commentdomain.Review) gin.H {
	payload := AdminComment(review.Comment)

	flags := make([]gin.H, 0, len(review.Flags))
	for _, flag := range review.Flags {
		flags = append(flags, gin.H{
			"reporter_id": uuidString(flag.ReporterUUID),
			"guest":       flag.ReporterUUID == nil,
			"reason_code": flag.Reason,
			"details":     emptyToNil(flag.Details),
			"created_at":  flag.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	history := make([]gin.H, 0, len(review.History))
	for _, entry := range review.History {
		history = append(history, ModerationEntry(entry))
	}

	payload["flags"] = flags
	payload["moderation_log"] = history

	return payload
}

// ModerationEntry serializes one moderation log row.
func ModerationEntry(entry commentdomain.ModerationEntry) gin.H {
	return gin.H{
		"id":            entry.UUID.String(),
		"comment_id":    entry.CommentUUID.String(),
		"moderator_id":  uuidString(entry.ModeratorUUID),
		"action":        string(entry.Action),
		"from_status":   string(entry.FromStatus),
		"to_status":     string(entry.ToStatus),
		"reason":        emptyToNil(entry.Reason),
		"notify_author": entry.NotifyAuthor,
		"before":        entry.Before,
		"after":         entry.After,
		"created_at":    entry.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// CommentStats serializes the moderation dashboard summary.
func CommentStats(stats commentdomain.Stats) gin.H {
	byStatus := gin.H{}
	for _, status := range []commentdomain.Status{
		commentdomain.StatusPending, commentdomain.StatusApproved, commentdomain.StatusFlagged,
		commentdomain.StatusSpam, commentdomain.StatusRejected, commentdomain.StatusDeleted,
	} {
		byStatus[string(status)] = stats.ByStatus[status]
	}

	posts := make([]gin.H, 0, len(stats.TopPosts))
	for _, post := range stats.TopPosts {
		posts = append(posts, gin.H{
			"post_id": post.PostUUID.String(),
			"title":   post.PostTitle,
			"pending": post.Pending,
			"flagged": post.Flagged,
		})
	}

	return gin.H{
		"by_status":        byStatus,
		"queue_depth":      stats.QueueDepth,
		"oldest_queued_at": RFC3339(stats.OldestQueuedAt),
		"top_posts":        posts,
	}
}

func uuidString(id *uuid.UUID) any {
	if id == nil {
		return nil
	}

	return id.String()
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}

	return value
}
