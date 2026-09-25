// Package service implements the audit recorder and activity queries.
package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/ports"
)

const (
	defaultQueueSize     = 1024
	defaultBatchSize     = 100
	defaultFlushInterval = time.Second
	insertTimeout        = 10 * time.Second
)

// RecorderOptions tunes the Recorder. Zero values use the defaults.
type RecorderOptions struct {
	QueueSize     int
	BatchSize     int
	FlushInterval time.Duration
	// OnDrop is called with the number of entries lost to a full queue or a failed insert.
	OnDrop func(n int)
	Now    func() time.Time
}

// Recorder writes entries from a bounded queue in batches on a background
// goroutine. Record never blocks: when the queue is full the entry is dropped,
// logged, and counted, so auditing cannot slow down or fail a request.
type Recorder struct {
	repo   ports.Repository
	logger *slog.Logger
	opts   RecorderOptions
	queue  chan domain.Entry
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

var _ ports.Writer = (*Recorder)(nil)

// NewRecorder starts the background writer. Call Close to flush and stop it.
func NewRecorder(repo ports.Repository, logger *slog.Logger, opts RecorderOptions) *Recorder {
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultQueueSize
	}

	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}

	if opts.FlushInterval <= 0 {
		opts.FlushInterval = defaultFlushInterval
	}

	if opts.OnDrop == nil {
		opts.OnDrop = func(int) {}
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	if logger == nil {
		logger = slog.Default()
	}

	r := &Recorder{
		repo:   repo,
		logger: logger,
		opts:   opts,
		queue:  make(chan domain.Entry, opts.QueueSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}

	go r.run()

	return r
}

// Record queues entry. It fills in the UUID and timestamp when missing.
func (r *Recorder) Record(_ context.Context, entry domain.Entry) {
	if entry.UUID == uuid.Nil {
		entry.UUID = uuid.New()
	}

	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = r.opts.Now().UTC()
	}

	select {
	case <-r.stop:
		r.drop(1, "audit recorder closed", entry.Action)
		return
	default:
	}

	select {
	case r.queue <- entry:
	default:
		r.drop(1, "audit queue full", entry.Action)
	}
}

// Close stops accepting entries, writes what is queued, and waits for the
// writer until ctx ends.
func (r *Recorder) Close(ctx context.Context) error {
	r.once.Do(func() { close(r.stop) })

	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Recorder) run() {
	defer close(r.done)

	ticker := time.NewTicker(r.opts.FlushInterval)
	defer ticker.Stop()

	batch := make([]domain.Entry, 0, r.opts.BatchSize)

	for {
		select {
		case entry := <-r.queue:
			batch = append(batch, entry)
			if len(batch) >= r.opts.BatchSize {
				batch = r.flush(batch)
			}
		case <-ticker.C:
			batch = r.flush(batch)
		case <-r.stop:
			r.drain(batch)
			return
		}
	}
}

func (r *Recorder) drain(batch []domain.Entry) {
	for {
		select {
		case entry := <-r.queue:
			batch = append(batch, entry)
			if len(batch) >= r.opts.BatchSize {
				batch = r.flush(batch)
			}
		default:
			r.flush(batch)
			return
		}
	}
}

func (r *Recorder) flush(batch []domain.Entry) []domain.Entry {
	if len(batch) == 0 {
		return batch
	}

	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()

	if err := r.repo.Insert(ctx, batch); err != nil {
		r.drop(len(batch), "audit insert failed", batch[0].Action, slog.Any("error", err))
	}

	return batch[:0]
}

func (r *Recorder) drop(n int, msg, action string, attrs ...any) {
	r.opts.OnDrop(n)
	r.logger.Warn(msg, append([]any{slog.Int("dropped", n), slog.String("action", action)}, attrs...)...)
}
