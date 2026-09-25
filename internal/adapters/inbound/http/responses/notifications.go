package responses

import (
	"github.com/gin-gonic/gin"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

// Notification is one inbox entry; data carries links such as postId, commentId, and url,
// stored in snake_case and renamed here.
func Notification(n notificationdomain.Notification) gin.H {
	data := camelKeys(n.Payload)

	return gin.H{
		"id":        n.UUID,
		"type":      n.Type,
		"title":     n.Title,
		"body":      n.Body,
		"preview":   n.Preview,
		"data":      data,
		"actorId":   n.ActorUUID,
		"isRead":    n.Read(),
		"readAt":    n.ReadAt,
		"createdAt": n.CreatedAt,
	}
}
