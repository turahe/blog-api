// Package analyticslive broadcasts accepted analytics events on the message broker so every
// API replica's live view counts every replica's traffic.
package analyticslive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

// Topic is the broker topic, before the MESSAGE_TOPIC_PREFIX is applied.
const Topic = "analytics.live"

// Batching of published events.
const (
	FlushEvery = time.Second
	batchSize  = 500
	queueSize  = 10_000
)

// wire is one event on the wire; field names are short because every accepted event is sent.
type wire struct {
	Kind    domain.Kind   `json:"k"`
	At      int64         `json:"t"` // Unix milliseconds
	Session uuid.UUID     `json:"s"`
	Path    string        `json:"p,omitempty"`
	Country string        `json:"c,omitempty"`
	Device  domain.Device `json:"d,omitempty"`
	Query   string        `json:"q,omitempty"`
	Results int           `json:"n,omitempty"`
}

type batch struct {
	Events []wire `json:"events"`
}

// Encode returns the wire form of events.
func Encode(events []domain.LiveEvent) ([]byte, error) {
	out := batch{Events: make([]wire, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, wire{
			Kind: e.Kind, At: e.At.UnixMilli(), Session: e.Session, Path: e.Path, Country: e.Country,
			Device: e.Device, Query: e.Query, Results: e.Results,
		})
	}

	return json.Marshal(out)
}

// Decode parses a wire batch, skipping events without a session or with an unknown kind.
func Decode(payload []byte) ([]domain.LiveEvent, error) {
	var in batch
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, fmt.Errorf("decode live analytics batch: %w", err)
	}

	out := make([]domain.LiveEvent, 0, len(in.Events))

	for _, e := range in.Events {
		switch e.Kind {
		case domain.KindPageView, domain.KindSearch, domain.KindTimeSpent, domain.KindNavigation, domain.KindSearchClick:
		default:
			continue
		}

		if e.Session == uuid.Nil {
			continue
		}

		out = append(out, domain.LiveEvent{
			Kind: e.Kind, At: time.UnixMilli(e.At).UTC(), Session: e.Session, Path: e.Path, Country: e.Country,
			Device: e.Device, Query: e.Query, Results: e.Results,
		})
	}

	return out, nil
}

// Publisher queues accepted events and publishes them in batches.
type Publisher struct {
	pub     message.Publisher
	topic   string
	logger  *slog.Logger
	queue   chan domain.LiveEvent
	dropped atomic.Int64
}

// NewPublisher returns a Publisher writing to topic; call Run to start publishing.
func NewPublisher(pub message.Publisher, topic string, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Publisher{pub: pub, topic: topic, logger: logger, queue: make(chan domain.LiveEvent, queueSize)}
}

// Publish queues event, or drops it when the queue is full.
func (p *Publisher) Publish(event domain.LiveEvent) {
	select {
	case p.queue <- event:
	default:
		p.dropped.Add(1)
	}
}

// Run publishes queued events every interval, or sooner when a batch fills, until ctx ends.
func (p *Publisher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pending := make([]domain.LiveEvent, 0, batchSize)

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-p.queue:
			if pending = append(pending, e); len(pending) >= batchSize {
				pending = p.flush(ctx, pending)
			}
		case <-ticker.C:
			pending = p.flush(ctx, pending)
		}
	}
}

// flush publishes pending and returns it emptied. A failure only costs the live view.
func (p *Publisher) flush(ctx context.Context, pending []domain.LiveEvent) []domain.LiveEvent {
	if dropped := p.dropped.Swap(0); dropped > 0 {
		p.logger.WarnContext(ctx, "analytics live: queue full, events dropped", "dropped", dropped)
	}

	if len(pending) == 0 {
		return pending
	}

	payload, err := Encode(pending)
	if err == nil {
		msg := message.NewMessage(watermill.NewUUID(), payload)
		msg.SetContext(ctx)
		err = p.pub.Publish(p.topic, msg)
	}

	if err != nil {
		p.logger.WarnContext(ctx, "analytics live: batch not published", "events", len(pending), "error", err)
	}

	return pending[:0]
}

// Consume subscribes to topic and passes every decoded batch to deliver until ctx ends or the
// subscriber closes. Undecodable messages are logged and acknowledged.
func Consume(
	ctx context.Context, sub message.Subscriber, topic string, deliver func(...domain.LiveEvent), logger *slog.Logger,
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
			events, err := Decode(msg.Payload)
			if err != nil {
				logger.Warn("analytics live: dropped malformed batch", "message_id", msg.UUID, "error", err)
			} else if len(events) > 0 {
				deliver(events...)
			}

			msg.Ack()
		}
	}()

	return nil
}
