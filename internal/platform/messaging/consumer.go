package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/turahe/blog-api/internal/platform/logging"
)

// DeadLetterTopic receives messages whose handler still fails after every retry.
const DeadLetterTopic = "blog.dead_letter"

// CorrelationIDKey is the metadata key carrying the originating request id.
const CorrelationIDKey = "correlation_id"

// ErrPermanent marks a handler error that retrying cannot fix; the message is
// dead-lettered on the first failure.
var ErrPermanent = errors.New("permanent failure")

// ConsumerConfig tunes handler retries before a message is dead-lettered.
type ConsumerConfig struct {
	MaxRetries      int
	InitialInterval time.Duration
	MaxInterval     time.Duration
}

// NewRouter returns a router whose handlers run behind, outermost first: the poison queue,
// correlation id propagation, tracing, retry with exponential backoff, and panic recovery.
// A message that exhausts its retries is published to DeadLetterTopic and acked.
func NewRouter(bus *Bus, cfg ConsumerConfig) (*message.Router, error) {
	router, err := message.NewRouter(message.RouterConfig{}, bus.Logger)
	if err != nil {
		return nil, fmt.Errorf("create message router: %w", err)
	}

	poison, err := middleware.PoisonQueue(bus.Publisher, bus.Topic(DeadLetterTopic))
	if err != nil {
		return nil, fmt.Errorf("create poison queue: %w", err)
	}

	retry := middleware.Retry{
		MaxRetries:      max(cfg.MaxRetries, 0),
		InitialInterval: cfg.InitialInterval,
		MaxInterval:     cfg.MaxInterval,
		Multiplier:      2,
		Logger:          bus.Logger,
		ShouldRetry: func(params middleware.RetryParams) bool {
			return !errors.Is(params.Err, ErrPermanent)
		},
	}

	router.AddMiddleware(poison, Correlation, Tracing, retry.Middleware, Recoverer)

	return router, nil
}

// Correlation copies the correlation_id metadata into the message context, so handler logs
// carry the request_id of the API call that produced the event.
func Correlation(h message.HandlerFunc) message.HandlerFunc {
	return func(msg *message.Message) ([]*message.Message, error) {
		if id := msg.Metadata.Get(CorrelationIDKey); id != "" {
			msg.SetContext(logging.WithRequestID(msg.Context(), id))
		}

		return h(msg)
	}
}

// Deduper runs fn at most once per consumer and message id. When fn fails the claim is
// released, so a redelivery runs it again.
type Deduper interface {
	Once(ctx context.Context, consumer, messageID string, fn func(ctx context.Context) error) error
}

// ErrMissingMessageID is returned for a message without a UUID; it cannot be deduplicated.
var ErrMissingMessageID = errors.New("message has no id")

// Idempotent wraps h so a message delivered again after it was handled is acked without
// running h. Message UUIDs are the outbox event ids, stable across redeliveries.
func Idempotent(d Deduper, consumer string, h message.NoPublishHandlerFunc) message.NoPublishHandlerFunc {
	return func(msg *message.Message) error {
		if msg.UUID == "" {
			return ErrMissingMessageID
		}

		return d.Once(msg.Context(), consumer, msg.UUID, func(ctx context.Context) error {
			msg.SetContext(ctx)

			return h(msg)
		})
	}
}
