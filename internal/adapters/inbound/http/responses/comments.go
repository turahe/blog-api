package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

// Comment serializes a comment resource. Deleted comments keep their place in the thread
// with content and author removed; author email and IP hash are never exposed.
func Comment(comment commentdomain.Comment) gin.H {
	var parentID any
	if comment.ParentUUID != nil {
		parentID = comment.ParentUUID.String()
	}

	content, contentHTML := comment.Content, comment.ContentHTML

	author := commentAuthor(comment)
	if comment.Status == commentdomain.StatusDeleted {
		content, contentHTML = "", ""
		author = nil
	}

	return gin.H{
		"id":           comment.UUID.String(),
		"post_id":      comment.PostUUID.String(),
		"parent_id":    parentID,
		"depth":        comment.Depth,
		"author":       author,
		"content":      content,
		"content_html": contentHTML,
		"status":       string(comment.Status),
		"upvote_count": comment.UpvoteCount,
		"reply_count":  comment.ReplyCount,
		"edited_at":    RFC3339(comment.EditedAt),
		"created_at":   comment.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":   comment.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// CommentThread serializes a comment with the first page of its replies.
func CommentThread(thread commentdomain.Thread) gin.H {
	payload := Comment(thread.Comment)

	replies := make([]gin.H, 0, len(thread.Replies.Items))
	for _, reply := range thread.Replies.Items {
		replies = append(replies, Comment(reply))
	}

	payload["replies"] = replies
	payload["replies_total"] = thread.Replies.Total

	return payload
}

func commentAuthor(comment commentdomain.Comment) any {
	if comment.AuthorUUID != nil {
		return gin.H{"id": comment.AuthorUUID.String(), "name": comment.AuthorUsername, "guest": false}
	}

	if comment.AuthorName != "" {
		return gin.H{"id": nil, "name": comment.AuthorName, "guest": true}
	}

	return nil
}
