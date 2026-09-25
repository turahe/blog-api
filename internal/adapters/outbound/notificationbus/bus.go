// Package notificationbus announces stored notifications on the message broker so every API
// replica can push them to its own SSE streams.
package notificationbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

// Topic is the broker topic, before the MESSAGE_TOPIC_PREFIX is applied.
const Topic = "notifications.created"

// event is the wire form. Dedupe keys stay server-side.
type event struct {
	ID        uuid.UUID         `json:"id"`
	UserID    uuid.UUID         `json:"user_id"`
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Preview   string            `json:"preview"`
	Data      map[string]string `json:"data"`
	ActorID   *uuid.UUID        `json:"actor_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Encode returns the wire form of n.
func Encode(n notificationdomain.Notification) ([]byte, error) {
	return json.Marshal(event{
		ID: n.UUID, UserID: n.UserUUID, Type: n.Type, Title: n.Title, Body: n.Body,
		Preview: n.Preview, Data: n.Payload, ActorID: n.ActorUUID, CreatedAt: n.CreatedAt,
	})
}

// Decode parses a wire message; a message without a recipient is rejected.
func Decode(payload []byte) (notificationdomain.Notification, error) {
	var e event
	if err := json.Unmarshal(payload, &e); err != nil {
		return notificationdomain.Notification{}, fmt.Errorf("decode notification event: %w", err)
	}

	if e.UserID == uuid.Nil || e.ID == uuid.Nil {
		return notificationdomain.Notification{}, errors.New("decode notification event: missing id or user_id")
	}

	return notificationdomain.Notification{
		UUID: e.ID, UserUUID: e.UserID, Type: e.Type, Title: e.Title, Body: e.Body,
		Preview: e.Preview, Payload: e.Data, ActorUUID: e.ActorID, CreatedAt: e.CreatedAt,
	}, nil
}

// Publisher announces stored notifications.
type Publisher struct {
	pub    message.Publisher
	topic  string
	logger *slog.Logger
}

// NewPublisher returns a Publisher writing to topic.
func NewPublisher(pub message.Publisher, topic string, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Publisher{pub: pub, topic: topic, logger: logger}
}

// Publish announces n. The notification is already stored, so a failure only costs the
// live push and is logged; clients still see it in the inbox.
func (p *Publisher) Publish(ctx context.Context, n notificationdomain.Notification) {
	payload, err := Encode(n)
	if err != nil {
		p.logger.WarnContext(ctx, "notify: live push not published", "type", n.Type, "error", err)
		return
	}

	msg := message.NewMessage(watermill.NewUUID(), payload)
	msg.SetContext(ctx)

	if err := p.pub.Publish(p.topic, msg); err != nil {
		p.logger.WarnContext(ctx, "notify: live push not published", "type", n.Type, "error", err)
	}
}

// Consume subscribes to topic and passes every decoded notification to deliver until ctx
// ends or the subscriber closes. Undecodable messages are logged and acknowledged.
func Consume(
	ctx context.Context,
	sub message.Subscriber,
	topic string,
	deliver func(notificationdomain.Notification),
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	messages, err := sub.Subscribe(ctx, topic)
	if err != nil {
		return fmt.Errorf("subscribe to %s: %w", topic, err)
	}

	go func() {
		for msg := range messages {
			n, err := Decode(msg.Payload)
			if err != nil {
				logger.Warn("notify: dropped malformed live push", "message_id", msg.UUID, "error", err)
			} else {
				deliver(n)
			}

			msg.Ack()
		}
	}()

	return nil
}
