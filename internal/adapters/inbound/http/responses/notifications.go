package responses

import (
	"github.com/gin-gonic/gin"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

// Notification is one inbox entry; data carries links such as post_id, comment_id, and url.
func Notification(n notificationdomain.Notification) gin.H {
	data := n.Payload
	if data == nil {
		data = map[string]string{}
	}

	return gin.H{
		"id":         n.UUID,
		"type":       n.Type,
		"title":      n.Title,
		"body":       n.Body,
		"preview":    n.Preview,
		"data":       data,
		"actor_id":   n.ActorUUID,
		"is_read":    n.Read(),
		"read_at":    n.ReadAt,
		"created_at": n.CreatedAt,
	}
}
