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
	payload["contentHtml"] = comment.ContentHTML
	payload["author"] = commentAuthor(comment)
	payload["authorEmail"] = emptyToNil(comment.AuthorEmail)
	payload["ipHash"] = emptyToNil(comment.IPHash)
	payload["userAgent"] = emptyToNil(comment.UserAgent)
	payload["flagCount"] = comment.FlagCount
	payload["moderatedBy"] = uuidString(comment.ModeratedByUUID)
	payload["moderationReason"] = emptyToNil(comment.ModerationReason)
	payload["moderatedAt"] = RFC3339(comment.ModeratedAt)
	payload["deletedAt"] = RFC3339(comment.DeletedAt)
	payload["deletedBy"] = uuidString(comment.DeletedByUUID)

	return payload
}

// CommentReview serializes a comment with its flags and moderation history.
func CommentReview(review commentdomain.Review) gin.H {
	payload := AdminComment(review.Comment)

	flags := make([]gin.H, 0, len(review.Flags))
	for _, flag := range review.Flags {
		flags = append(flags, gin.H{
			"reporterId": uuidString(flag.ReporterUUID),
			"guest":      flag.ReporterUUID == nil,
			"reasonCode": flag.Reason,
			"details":    emptyToNil(flag.Details),
			"createdAt":  flag.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	history := make([]gin.H, 0, len(review.History))
	for _, entry := range review.History {
		history = append(history, ModerationEntry(entry))
	}

	payload["flags"] = flags
	payload["moderationLog"] = history

	return payload
}

// ModerationEntry serializes one moderation log row.
func ModerationEntry(entry commentdomain.ModerationEntry) gin.H {
	return gin.H{
		"id":           entry.UUID.String(),
		"commentId":    entry.CommentUUID.String(),
		"moderatorId":  uuidString(entry.ModeratorUUID),
		"action":       string(entry.Action),
		"fromStatus":   string(entry.FromStatus),
		"toStatus":     string(entry.ToStatus),
		"reason":       emptyToNil(entry.Reason),
		"notifyAuthor": entry.NotifyAuthor,
		"before":       entry.Before,
		"after":        entry.After,
		"createdAt":    entry.CreatedAt.UTC().Format(time.RFC3339),
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
			"postId":  post.PostUUID.String(),
			"title":   post.PostTitle,
			"pending": post.Pending,
			"flagged": post.Flagged,
		})
	}

	return gin.H{
		"byStatus":       byStatus,
		"queueDepth":     stats.QueueDepth,
		"oldestQueuedAt": RFC3339(stats.OldestQueuedAt),
		"topPosts":       posts,
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
