// Package outbox relays rows from the transactional outbox to the message broker.
package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
)

// Store is the outbox table as the relay sees it.
type Store interface {
	RelayBatch(ctx context.Context, now time.Time, limit int,
		publish func(persistence.OutboxRecord) persistence.OutboxOutcome) (int, error)
	PruneBefore(ctx context.Context, cutoff time.Time) (int64, error)
	Backlog(ctx context.Context) (persistence.OutboxBacklog, error)
}

// Observer receives relay counters, for metrics.
type Observer interface {
	OutboxPublished()
	OutboxPublishFailed(terminal bool)
	OutboxBacklog(pending, failed int64, oldestPendingAt *time.Time, now time.Time)
}

// Config tunes the relay. Zero fields take the defaults noted on each.
type Config struct {
	BatchSize     int           // rows claimed per transaction; 100
	PollInterval  time.Duration // wait when nothing is due; 1s
	MaxAttempts   int           // publishes before a row is parked as failed; 10
	BaseBackoff   time.Duration // first retry delay, doubled per attempt; 1s
	MaxBackoff    time.Duration // retry delay cap; 10m
	Retention     time.Duration // how long published rows are kept; 7 days
	StatsInterval time.Duration // backlog sampling and pruning period; 30s
}

func (c Config) withDefaults() Config {
	if c.BatchSize < 1 {
		c.BatchSize = 100
	}

	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}

	if c.MaxAttempts < 1 {
		c.MaxAttempts = 10
	}

	if c.BaseBackoff <= 0 {
		c.BaseBackoff = time.Second
	}

	if c.MaxBackoff < c.BaseBackoff {
		c.MaxBackoff = max(10*time.Minute, c.BaseBackoff)
	}

	if c.Retention <= 0 {
		c.Retention = 7 * 24 * time.Hour
	}

	if c.StatsInterval <= 0 {
		c.StatsInterval = 30 * time.Second
	}

	return c
}

// Relay publishes due outbox rows. Several relays may run at once: rows are claimed with
// SKIP LOCKED, so each is published by one relay at a time. Delivery is at least once: a
// crash between publishing and committing republishes the row.
type Relay struct {
	store     Store
	publisher message.Publisher
	topic     func(string) string
	cfg       Config
	logger    *slog.Logger
	observer  Observer
	now       func() time.Time
}

// New returns a relay publishing through publisher; topic maps an event type to the
// broker topic (for example messaging.Bus.Topic, which applies MESSAGE_TOPIC_PREFIX).
func New(store Store, publisher message.Publisher, topic func(string) string, cfg Config, logger *slog.Logger) *Relay {
	if logger == nil {
		logger = slog.Default()
	}

	return &Relay{
		store: store, publisher: publisher, topic: topic, cfg: cfg.withDefaults(), logger: logger,
		observer: noopObserver{}, now: time.Now,
	}
}

// WithObserver reports counters to observer.
func (r *Relay) WithObserver(observer Observer) *Relay {
	if observer != nil {
		r.observer = observer
	}

	return r
}

// Run relays until ctx is done.
func (r *Relay) Run(ctx context.Context) {
	stats := time.NewTicker(r.cfg.StatsInterval)
	defer stats.Stop()

	poll := time.NewTimer(0)
	defer poll.Stop()

	r.maintain(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-stats.C:
			r.maintain(ctx)
		case <-poll.C:
			if _, err := r.Drain(ctx); err != nil && ctx.Err() == nil {
				r.logger.ErrorContext(ctx, "outbox relay failed", "error", err)
			}

			poll.Reset(r.cfg.PollInterval)
		}
	}
}

// Drain relays batches until fewer than a full batch is due, and returns how many rows
// it claimed.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	total := 0

	for ctx.Err() == nil {
		claimed, err := r.store.RelayBatch(ctx, r.now().UTC(), r.cfg.BatchSize, func(rec persistence.OutboxRecord) persistence.OutboxOutcome {
			return r.publish(ctx, rec)
		})
		total += claimed

		if err != nil || claimed < r.cfg.BatchSize {
			return total, err
		}
	}

	return total, ctx.Err()
}

func (r *Relay) publish(ctx context.Context, rec persistence.OutboxRecord) persistence.OutboxOutcome {
	msg := message.NewMessage(rec.UUID.String(), rec.Payload)
	for k, v := range rec.Headers {
		msg.Metadata.Set(k, v)
	}

	msg.SetContext(ctx)

	err := r.publisher.Publish(r.topic(rec.Topic), msg)
	if err == nil {
		r.observer.OutboxPublished()
		return persistence.OutboxOutcome{}
	}

	attempt := rec.Attempts + 1
	terminal := attempt >= r.cfg.MaxAttempts
	r.observer.OutboxPublishFailed(terminal)

	attrs := []any{"event_id", rec.UUID, "type", rec.Topic, "attempt", attempt, "error", err}
	if terminal {
		r.logger.ErrorContext(ctx, "outbox event parked after last attempt", attrs...)
	} else {
		r.logger.WarnContext(ctx, "outbox publish failed; will retry", attrs...)
	}

	return persistence.OutboxOutcome{
		Err:           err,
		NextAttemptAt: r.now().UTC().Add(Backoff(attempt, r.cfg.BaseBackoff, r.cfg.MaxBackoff)),
		Failed:        terminal,
	}
}

func (r *Relay) maintain(ctx context.Context) {
	now := r.now().UTC()

	backlog, err := r.store.Backlog(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logger.WarnContext(ctx, "outbox backlog query failed", "error", err)
		}

		return
	}

	r.observer.OutboxBacklog(backlog.Pending, backlog.Failed, backlog.OldestPendingAt, now)

	pruned, err := r.store.PruneBefore(ctx, now.Add(-r.cfg.Retention))
	if err != nil {
		r.logger.WarnContext(ctx, "outbox prune failed", "error", err)
		return
	}

	if pruned > 0 {
		r.logger.InfoContext(ctx, "outbox pruned", "rows", pruned)
	}
}

// Backoff is the delay before retry attempt+1: base doubled per attempt, capped at ceiling.
func Backoff(attempt int, base, ceiling time.Duration) time.Duration {
	delay := base

	for i := 1; i < attempt && delay < ceiling; i++ {
		delay *= 2
	}

	return min(delay, ceiling)
}

type noopObserver struct{}

func (noopObserver) OutboxPublished()                                  {}
func (noopObserver) OutboxPublishFailed(bool)                          {}
func (noopObserver) OutboxBacklog(int64, int64, *time.Time, time.Time) {}
