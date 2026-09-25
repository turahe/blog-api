package service_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit/domain"
	"github.com/turahe/blog-api/internal/core/audit/service"
)

type memRepo struct {
	mu      sync.Mutex
	batches [][]domain.Entry
	fail    error
	block   chan struct{} // when set, Insert waits for it to close
	cutoff  time.Time
	filter  domain.ActivityFilter
}

func (r *memRepo) Insert(_ context.Context, entries []domain.Entry) error {
	if r.block != nil {
		<-r.block
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail != nil {
		return r.fail
	}

	r.batches = append(r.batches, append([]domain.Entry(nil), entries...))

	return nil
}

func (r *memRepo) Activity(_ context.Context, filter domain.ActivityFilter) (domain.ActivityPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.filter = filter

	return domain.ActivityPage{Page: filter.Page, PerPage: filter.PerPage}, nil
}

func (r *memRepo) Prune(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cutoff = cutoff

	return 3, nil
}

func (r *memRepo) stored() []domain.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	var all []domain.Entry
	for _, batch := range r.batches {
		all = append(all, batch...)
	}

	return all
}

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func closeRecorder(t *testing.T, r *service.Recorder) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, r.Close(ctx))
}

func TestRecorderFlushesQueuedEntriesOnClose(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	r := service.NewRecorder(repo, quietLogger(), service.RecorderOptions{
		BatchSize: 2, FlushInterval: time.Hour, Now: func() time.Time { return now },
	})

	for range 5 {
		r.Record(context.Background(), domain.Entry{Action: "admin.posts.create"})
	}

	closeRecorder(t, r)

	stored := repo.stored()
	require.Len(t, stored, 5)

	for _, entry := range stored {
		require.NotEqual(t, uuid.Nil, entry.UUID)
		require.Equal(t, now, entry.OccurredAt)
	}
}

func TestRecorderFlushesOnInterval(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	r := service.NewRecorder(repo, quietLogger(), service.RecorderOptions{FlushInterval: 10 * time.Millisecond})

	t.Cleanup(func() { closeRecorder(t, r) })

	r.Record(context.Background(), domain.Entry{Action: "auth.logout"})

	require.Eventually(t, func() bool { return len(repo.stored()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestRecorderDropsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	var dropped atomic.Int64

	repo := &memRepo{block: make(chan struct{})}
	r := service.NewRecorder(repo, quietLogger(), service.RecorderOptions{
		QueueSize: 1, BatchSize: 1, FlushInterval: time.Hour,
		OnDrop: func(n int) { dropped.Add(int64(n)) },
	})

	// The writer takes the first entry and blocks in Insert; the second fills the
	// queue; the rest are dropped without blocking the caller.
	r.Record(context.Background(), domain.Entry{Action: "a"})
	require.Eventually(t, func() bool {
		r.Record(context.Background(), domain.Entry{Action: "b"})
		return dropped.Load() > 0
	}, 2*time.Second, time.Millisecond)

	close(repo.block)
	closeRecorder(t, r)

	require.Positive(t, dropped.Load())
	require.NotEmpty(t, repo.stored(), "queued entries are still written once the database recovers")
}

func TestRecorderCountsFailedInsertsAsDropped(t *testing.T) {
	t.Parallel()

	var dropped atomic.Int64

	repo := &memRepo{fail: errors.New("db down")}
	r := service.NewRecorder(repo, quietLogger(), service.RecorderOptions{
		FlushInterval: time.Hour, OnDrop: func(n int) { dropped.Add(int64(n)) },
	})

	r.Record(context.Background(), domain.Entry{Action: "a"})
	r.Record(context.Background(), domain.Entry{Action: "b"})
	closeRecorder(t, r)

	require.Equal(t, int64(2), dropped.Load())
}

func TestRecorderDropsAfterClose(t *testing.T) {
	t.Parallel()

	var dropped atomic.Int64

	repo := &memRepo{}
	r := service.NewRecorder(repo, quietLogger(), service.RecorderOptions{OnDrop: func(n int) { dropped.Add(int64(n)) }})
	closeRecorder(t, r)

	r.Record(context.Background(), domain.Entry{Action: "late"})

	require.Equal(t, int64(1), dropped.Load())
	require.Empty(t, repo.stored())
	require.NoError(t, r.Close(context.Background()), "Close is idempotent")
}
