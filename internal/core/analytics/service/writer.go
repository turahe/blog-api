package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

const (
	defaultQueueSize     = 10_000
	defaultBatchSize     = 500
	defaultFlushInterval = time.Second
	insertTimeout        = 10 * time.Second
	dropLogInterval      = 10 * time.Second
)

// WriterOptions tunes the Writer. Zero values use the defaults.
type WriterOptions struct {
	QueueSize     int
	BatchSize     int
	FlushInterval time.Duration
	// OnDrop is called with the number of events lost to a full queue or a failed insert.
	OnDrop func(n int)
}

// Writer stores events from a bounded queue in batches on a background goroutine. Enqueue
// never blocks: when the queue is full the event is dropped and counted, so a slow database
// slows nothing but the writer. Events still queued when the process dies are lost.
type Writer struct {
	repo   ports.EventRepository
	logger *slog.Logger
	opts   WriterOptions
	queue  chan domain.Event
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	// lastLog is when a drop was last logged, in Unix nanoseconds.
	lastLog atomic.Int64
}

var _ ports.Sink = (*Writer)(nil)

// NewWriter starts the background writer. Call Close to flush and stop it.
func NewWriter(repo ports.EventRepository, logger *slog.Logger, opts WriterOptions) *Writer {
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

	if logger == nil {
		logger = slog.Default()
	}

	w := &Writer{
		repo:   repo,
		logger: logger,
		opts:   opts,
		queue:  make(chan domain.Event, opts.QueueSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}

	go w.run()

	return w
}

// Enqueue queues event, or drops it when the queue is full or the writer is closed.
func (w *Writer) Enqueue(event domain.Event) bool {
	select {
	case <-w.stop:
		w.drop(1, "analytics writer closed")
		return false
	default:
	}

	select {
	case w.queue <- event:
		return true
	default:
		w.drop(1, "analytics queue full")
		return false
	}
}

// Close stops accepting events, writes what is queued, and waits for the writer until ctx
// ends.
func (w *Writer) Close(ctx context.Context) error {
	w.once.Do(func() { close(w.stop) })

	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.opts.FlushInterval)
	defer ticker.Stop()

	batch := make([]domain.Event, 0, w.opts.BatchSize)

	for {
		select {
		case event := <-w.queue:
			batch = append(batch, event)
			if len(batch) >= w.opts.BatchSize {
				batch = w.flush(batch)
			}
		case <-ticker.C:
			batch = w.flush(batch)
		case <-w.stop:
			w.drain(batch)
			return
		}
	}
}

func (w *Writer) drain(batch []domain.Event) {
	for {
		select {
		case event := <-w.queue:
			batch = append(batch, event)
			if len(batch) >= w.opts.BatchSize {
				batch = w.flush(batch)
			}
		default:
			w.flush(batch)
			return
		}
	}
}

func (w *Writer) flush(batch []domain.Event) []domain.Event {
	if len(batch) == 0 {
		return batch
	}

	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()

	if err := w.repo.InsertBatch(ctx, batch); err != nil {
		w.drop(len(batch), "analytics insert failed", slog.Any("error", err))
	}

	return batch[:0]
}

// drop counts lost events and logs at most once per dropLogInterval, with counts only: event
// payloads never reach the logs.
func (w *Writer) drop(n int, msg string, attrs ...any) {
	w.opts.OnDrop(n)

	now := time.Now().UnixNano()
	last := w.lastLog.Load()

	if now-last < int64(dropLogInterval) || !w.lastLog.CompareAndSwap(last, now) {
		return
	}

	w.logger.Warn(msg, append([]any{slog.Int("dropped", n)}, attrs...)...)
}
