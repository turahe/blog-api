package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type call struct {
	name  string
	every time.Duration
}

type fakeStore struct {
	mu    sync.Mutex
	calls []call
	busy  bool
}

func (f *fakeStore) TryRun(ctx context.Context, name string, every time.Duration, _ time.Time, fn func(context.Context) error) (bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{name: name, every: every})
	busy := f.busy
	f.mu.Unlock()

	if busy {
		return false, nil
	}

	return true, fn(ctx)
}

func (f *fakeStore) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := 0

	for _, c := range f.calls {
		if c.name == name {
			n++
		}
	}

	return n
}

func quiet() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestRunChecksEveryJobUntilCancelled(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	failing := errors.New("db down")
	s := New(store, 5*time.Millisecond, quiet(),
		Job{Name: "a", Every: time.Hour, Run: func(context.Context) error { return nil }},
		Job{Name: "b", Every: time.Millisecond, Run: func(context.Context) error { return failing }},
	)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	require.Eventually(t, func() bool { return store.count("a") >= 2 && store.count("b") >= 2 },
		2*time.Second, time.Millisecond, "a failing job keeps being checked")

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, c := range store.calls {
		if c.name == "a" {
			require.Equal(t, time.Hour, c.every, "the loop passes the job interval to the store")
		}
	}
}

func TestRunNowForcesTheNamedJob(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	ran := false
	s := New(store, time.Minute, quiet(), Job{Name: "prune", Every: time.Hour, Run: func(context.Context) error {
		ran = true

		return nil
	}})

	ok, err := s.RunNow(t.Context(), "prune")
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, ran)
	require.Equal(t, []call{{name: "prune", every: 0}}, store.calls)

	store.busy = true
	ok, err = s.RunNow(t.Context(), "prune")
	require.NoError(t, err)
	require.False(t, ok, "a run in progress elsewhere is not overridden")

	_, err = s.RunNow(t.Context(), "missing")
	require.ErrorIs(t, err, ErrUnknownJob)
}
