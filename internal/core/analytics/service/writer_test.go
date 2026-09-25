package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

type memEventRepo struct {
	mu      sync.Mutex
	batches [][]domain.Event
	err     error
	block   chan struct{}
}

func (r *memEventRepo) InsertBatch(ctx context.Context, events []domain.Event) error {
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.batches = append(r.batches, append([]domain.Event(nil), events...))

	return r.err
}

func (r *memEventRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := 0
	for _, b := range r.batches {
		n += len(b)
	}

	return n
}

func event() domain.Event {
	return domain.Event{Kind: domain.KindPageView, UUID: uuid.New(), PageView: &domain.PageView{Path: "/"}}
}

func TestWriterFlushesInBatchesAndOnClose(t *testing.T) {
	t.Parallel()

	repo := &memEventRepo{}
	w := NewWriter(repo, nil, WriterOptions{BatchSize: 2, FlushInterval: time.Hour})

	for range 5 {
		require.True(t, w.Enqueue(event()))
	}

	require.Eventually(t, func() bool { return repo.count() >= 4 }, time.Second, 5*time.Millisecond, "full batches flush at once")
	require.NoError(t, w.Close(t.Context()))
	assert.Equal(t, 5, repo.count(), "Close writes the remainder")
	assert.False(t, w.Enqueue(event()), "a closed writer drops events")
}

func TestWriterFlushesOnTheInterval(t *testing.T) {
	t.Parallel()

	repo := &memEventRepo{}
	w := NewWriter(repo, nil, WriterOptions{BatchSize: 100, FlushInterval: 10 * time.Millisecond})

	t.Cleanup(func() { _ = w.Close(context.Background()) })

	w.Enqueue(event())
	require.Eventually(t, func() bool { return repo.count() == 1 }, time.Second, 5*time.Millisecond)
}

func TestWriterDropsWhenFullWithoutBlocking(t *testing.T) {
	t.Parallel()

	var dropped atomic.Int64

	repo := &memEventRepo{block: make(chan struct{})}
	w := NewWriter(repo, nil, WriterOptions{
		QueueSize: 1, BatchSize: 1, FlushInterval: time.Hour, OnDrop: func(n int) { dropped.Add(int64(n)) },
	})

	// The first event is taken by the (blocked) insert, the second fills the queue.
	require.True(t, w.Enqueue(event()))
	require.Eventually(t, func() bool { return w.Enqueue(event()) }, time.Second, time.Millisecond)

	before := dropped.Load()
	start := time.Now()

	for range 10 {
		assert.False(t, w.Enqueue(event()))
	}

	assert.Less(t, time.Since(start), 100*time.Millisecond, "Enqueue never waits for the database")
	assert.Equal(t, int64(10), dropped.Load()-before)

	close(repo.block)
	require.NoError(t, w.Close(t.Context()))
	assert.Equal(t, 2, repo.count())
}

func TestWriterCountsFailedInserts(t *testing.T) {
	t.Parallel()

	var dropped atomic.Int64

	repo := &memEventRepo{err: errors.New("db down")}
	w := NewWriter(repo, nil, WriterOptions{BatchSize: 3, FlushInterval: time.Hour, OnDrop: func(n int) { dropped.Add(int64(n)) }})

	for range 3 {
		w.Enqueue(event())
	}

	require.NoError(t, w.Close(t.Context()))
	assert.Equal(t, int64(3), dropped.Load())
}
