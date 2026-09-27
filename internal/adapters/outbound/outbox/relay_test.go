package outbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
)

type fakeStore struct {
	mu       sync.Mutex
	pending  []persistence.OutboxRecord
	outcomes map[uuid.UUID]persistence.OutboxOutcome
	batches  int
}

func (s *fakeStore) RelayBatch(_ context.Context, _ time.Time, limit int,
	publish func(persistence.OutboxRecord) persistence.OutboxOutcome,
) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.batches++
	n := min(limit, len(s.pending))

	for _, rec := range s.pending[:n] {
		s.outcomes[rec.UUID] = publish(rec)
	}

	s.pending = s.pending[n:]

	return n, nil
}

func (s *fakeStore) PruneBefore(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *fakeStore) Backlog(context.Context) (persistence.OutboxBacklog, error) {
	return persistence.OutboxBacklog{}, nil
}

type failingPublisher struct{ err error }

func (p failingPublisher) Publish(string, ...*message.Message) error { return p.err }
func (failingPublisher) Close() error                                { return nil }

func record(attempts int) persistence.OutboxRecord {
	id := uuid.New()

	return persistence.OutboxRecord{
		UUID: id, Topic: "blog.post.created", Payload: []byte(`{"n":1}`), Attempts: attempts,
		Headers: map[string]string{persistence.HeaderID: id.String(), persistence.HeaderType: "blog.post.created"},
	}
}

// scriptedStore records when the relay calls it and returns configured results.
type scriptedStore struct {
	mu           sync.Mutex
	relayCalls   []time.Time
	backlogCalls []time.Time
	pruneCutoffs []time.Time
	relay        func(ctx context.Context) (int, error)
	backlog      persistence.OutboxBacklog
	backlogErr   error
	pruned       int64
	pruneErr     error
}

func (s *scriptedStore) RelayBatch(ctx context.Context, now time.Time, _ int,
	_ func(persistence.OutboxRecord) persistence.OutboxOutcome,
) (int, error) {
	s.mu.Lock()
	s.relayCalls = append(s.relayCalls, now)
	relay := s.relay
	s.mu.Unlock()

	if relay == nil {
		return 0, nil
	}

	return relay(ctx)
}

func (s *scriptedStore) PruneBefore(_ context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneCutoffs = append(s.pruneCutoffs, cutoff)

	return s.pruned, s.pruneErr
}

func (s *scriptedStore) Backlog(context.Context) (persistence.OutboxBacklog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.backlogCalls = append(s.backlogCalls, time.Now().UTC())

	return s.backlog, s.backlogErr
}

func (s *scriptedStore) counts() (relays, backlogs int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.relayCalls), len(s.backlogCalls)
}

type backlogSample struct {
	pending, failed int64
	oldest          *time.Time
	now             time.Time
}

type recordingObserver struct {
	mu        sync.Mutex
	published int
	failures  []bool
	backlogs  []backlogSample
}

func (o *recordingObserver) OutboxPublished() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.published++
}

func (o *recordingObserver) OutboxPublishFailed(terminal bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.failures = append(o.failures, terminal)
}

func (o *recordingObserver) OutboxBacklog(pending, failed int64, oldest *time.Time, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.backlogs = append(o.backlogs, backlogSample{pending: pending, failed: failed, oldest: oldest, now: now})
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func bufferLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}

	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func TestConfigWithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "zero config takes defaults",
			in:   Config{},
			want: Config{
				BatchSize: 100, PollInterval: time.Second, MaxAttempts: 10, BaseBackoff: time.Second,
				MaxBackoff: 10 * time.Minute, Retention: 7 * 24 * time.Hour, StatsInterval: 30 * time.Second,
			},
		},
		{
			name: "explicit values kept",
			in: Config{
				BatchSize: 5, PollInterval: 2 * time.Second, MaxAttempts: 3, BaseBackoff: 3 * time.Second,
				MaxBackoff: time.Minute, Retention: time.Hour, StatsInterval: time.Minute,
			},
			want: Config{
				BatchSize: 5, PollInterval: 2 * time.Second, MaxAttempts: 3, BaseBackoff: 3 * time.Second,
				MaxBackoff: time.Minute, Retention: time.Hour, StatsInterval: time.Minute,
			},
		},
		{
			name: "ceiling below base is raised",
			in:   Config{BaseBackoff: time.Hour, MaxBackoff: time.Minute},
			want: Config{
				BatchSize: 100, PollInterval: time.Second, MaxAttempts: 10, BaseBackoff: time.Hour,
				MaxBackoff: time.Hour, Retention: 7 * 24 * time.Hour, StatsInterval: 30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.in.withDefaults())
		})
	}
}

func TestRelayWithObserver(t *testing.T) {
	t.Parallel()

	t.Run("nil keeps noop observer", func(t *testing.T) {
		t.Parallel()

		relay := New(&scriptedStore{}, failingPublisher{}, func(s string) string { return s }, Config{}, nil)

		require.Same(t, relay, relay.WithObserver(nil))
		require.Equal(t, noopObserver{}, relay.observer)
	})

	t.Run("observer receives publish counters", func(t *testing.T) {
		t.Parallel()

		ok, retry, last := record(0), record(0), record(9)
		store := &fakeStore{pending: []persistence.OutboxRecord{ok}, outcomes: map[uuid.UUID]persistence.OutboxOutcome{}}
		observer := &recordingObserver{}

		pubsub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
		t.Cleanup(func() { _ = pubsub.Close() })

		_, err := New(store, pubsub, func(s string) string { return s }, Config{}, nil).WithObserver(observer).Drain(t.Context())
		require.NoError(t, err)

		store.pending = []persistence.OutboxRecord{retry, last}
		_, err = New(store, failingPublisher{err: errors.New("down")}, func(s string) string { return s }, Config{}, nil).
			WithObserver(observer).Drain(t.Context())
		require.NoError(t, err)

		require.Equal(t, 1, observer.published)
		require.Equal(t, []bool{false, true}, observer.failures)
	})
}

func TestRelayRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		store := &scriptedStore{}
		relay := New(store, failingPublisher{}, func(s string) string { return s },
			Config{PollInterval: time.Second, StatsInterval: 30 * time.Second}, nil)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)
			relay.Run(ctx)
		}()

		synctest.Wait()

		relays, backlogs := store.counts()
		require.Equal(t, 1, relays, "first drain runs immediately")
		require.Equal(t, 1, backlogs, "stats are sampled on start")

		time.Sleep(time.Second)
		synctest.Wait()

		relays, backlogs = store.counts()
		require.Equal(t, 2, relays, "next drain after the poll interval")
		require.Equal(t, 1, backlogs)

		time.Sleep(29 * time.Second)
		synctest.Wait()

		relays, backlogs = store.counts()
		require.Equal(t, 31, relays)
		require.Equal(t, 2, backlogs, "stats resampled on the stats tick")

		cancel()
		synctest.Wait()

		select {
		case <-done:
		default:
			t.Fatal("Run did not return after cancel")
		}

		relays, backlogs = store.counts()
		require.Equal(t, 31, relays, "no drains after cancel")
		require.Equal(t, 2, backlogs)
	})
}

func TestRelayRunDrainErrors(t *testing.T) {
	t.Parallel()

	errStore := errors.New("store unavailable")

	tests := []struct {
		name    string
		relay   func(ctx context.Context, cancel context.CancelFunc) (int, error)
		wantLog bool
	}{
		{
			name:    "store error is logged",
			relay:   func(context.Context, context.CancelFunc) (int, error) { return 0, errStore },
			wantLog: true,
		},
		{
			name: "error after cancel is not logged",
			relay: func(ctx context.Context, cancel context.CancelFunc) (int, error) {
				cancel()
				return 0, ctx.Err()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				store := &scriptedStore{relay: func(ctx context.Context) (int, error) { return tt.relay(ctx, cancel) }}
				logger, logs := bufferLogger()
				relay := New(store, failingPublisher{}, func(s string) string { return s }, Config{}, logger)

				done := make(chan struct{})

				go func() {
					defer close(done)
					relay.Run(ctx)
				}()

				synctest.Wait()
				cancel()
				<-done

				if tt.wantLog {
					require.Contains(t, logs.String(), `msg="outbox relay failed" error="store unavailable"`)
				} else {
					require.NotContains(t, logs.String(), "outbox relay failed")
				}
			})
		})
	}
}

func TestRelayPublishesEnvelope(t *testing.T) {
	t.Parallel()

	pubsub := gochannel.NewGoChannel(gochannel.Config{Persistent: true}, watermill.NopLogger{})

	t.Cleanup(func() { _ = pubsub.Close() })

	messages, err := pubsub.Subscribe(t.Context(), "test.blog.post.created")
	require.NoError(t, err)

	rec := record(0)
	store := &fakeStore{pending: []persistence.OutboxRecord{rec}, outcomes: map[uuid.UUID]persistence.OutboxOutcome{}}
	relay := New(store, pubsub, func(topic string) string { return "test." + topic }, Config{}, nil)

	claimed, err := relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, claimed)
	require.NoError(t, store.outcomes[rec.UUID].Err)

	select {
	case msg := <-messages:
		require.Equal(t, rec.UUID.String(), msg.UUID)
		require.JSONEq(t, `{"n":1}`, string(msg.Payload))
		require.Equal(t, "blog.post.created", msg.Metadata.Get(persistence.HeaderType))
		require.Equal(t, rec.UUID.String(), msg.Metadata.Get(persistence.HeaderID))
		msg.Ack()
	case <-time.After(5 * time.Second):
		t.Fatal("message not published")
	}
}

func TestRelayBacksOffThenParks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	first, last := record(0), record(4)
	store := &fakeStore{pending: []persistence.OutboxRecord{first, last}, outcomes: map[uuid.UUID]persistence.OutboxOutcome{}}

	relay := New(store, failingPublisher{err: errors.New("broker down")}, func(s string) string { return s },
		Config{MaxAttempts: 5, BaseBackoff: time.Second, MaxBackoff: time.Minute}, nil)
	relay.now = func() time.Time { return now }

	_, err := relay.Drain(t.Context())
	require.NoError(t, err)

	retry := store.outcomes[first.UUID]
	require.EqualError(t, retry.Err, "broker down")
	require.False(t, retry.Failed)
	require.Equal(t, now.Add(time.Second), retry.NextAttemptAt)

	parked := store.outcomes[last.UUID]
	require.Error(t, parked.Err)
	require.True(t, parked.Failed, "fifth attempt is the last")
}

func TestRelayDrainsFullBatches(t *testing.T) {
	t.Parallel()

	store := &fakeStore{outcomes: map[uuid.UUID]persistence.OutboxOutcome{}}
	for range 5 {
		store.pending = append(store.pending, record(0))
	}

	pubsub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})

	t.Cleanup(func() { _ = pubsub.Close() })

	claimed, err := New(store, pubsub, func(s string) string { return s }, Config{BatchSize: 2}, nil).Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 5, claimed)
	require.Equal(t, 3, store.batches, "2 + 2 + 1, stopping at the short batch")
}

func TestRelayDrainStops(t *testing.T) {
	t.Parallel()

	errStore := errors.New("store unavailable")

	tests := []struct {
		name      string
		relay     func(cancel context.CancelFunc) (int, error)
		preCancel bool
		wantTotal int
		wantCalls int
		wantErr   error
	}{
		{
			name:      "store error returns claimed so far",
			relay:     func(context.CancelFunc) (int, error) { return 1, errStore },
			wantTotal: 1, wantCalls: 1, wantErr: errStore,
		},
		{
			name: "cancel between full batches",
			relay: func(cancel context.CancelFunc) (int, error) {
				cancel()
				return 2, nil
			},
			wantTotal: 2, wantCalls: 1, wantErr: context.Canceled,
		},
		{
			name:      "already cancelled claims nothing",
			relay:     func(context.CancelFunc) (int, error) { return 2, nil },
			preCancel: true,
			wantTotal: 0, wantCalls: 0, wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.preCancel {
				cancel()
			}

			store := &scriptedStore{relay: func(context.Context) (int, error) { return tt.relay(cancel) }}

			total, err := New(store, failingPublisher{}, func(s string) string { return s }, Config{BatchSize: 2}, nil).Drain(ctx)

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantTotal, total)

			relays, _ := store.counts()
			require.Equal(t, tt.wantCalls, relays)
		})
	}
}

func TestRelayMaintain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	oldest := now.Add(-time.Minute)
	errDB := errors.New("db down")

	tests := []struct {
		name        string
		store       *scriptedStore
		wantBacklog []backlogSample
		wantPrunes  int
		wantLog     string
		wantNoLog   bool
	}{
		{
			name:        "samples backlog and prunes nothing",
			store:       &scriptedStore{backlog: persistence.OutboxBacklog{Pending: 3, Failed: 1, OldestPendingAt: &oldest}},
			wantBacklog: []backlogSample{{pending: 3, failed: 1, oldest: &oldest, now: now.UTC()}},
			wantPrunes:  1,
			wantNoLog:   true,
		},
		{
			name:        "logs pruned rows",
			store:       &scriptedStore{pruned: 4},
			wantBacklog: []backlogSample{{now: now.UTC()}},
			wantPrunes:  1,
			wantLog:     `level=INFO msg="outbox pruned" rows=4`,
		},
		{
			name:        "prune failure is logged",
			store:       &scriptedStore{pruneErr: errDB},
			wantBacklog: []backlogSample{{now: now.UTC()}},
			wantPrunes:  1,
			wantLog:     `level=WARN msg="outbox prune failed" error="db down"`,
		},
		{
			name:    "backlog failure skips prune",
			store:   &scriptedStore{backlogErr: errDB},
			wantLog: `level=WARN msg="outbox backlog query failed" error="db down"`,
		},
		{
			name:      "cancelled backlog query is silent",
			store:     &scriptedStore{backlogErr: context.Canceled},
			wantNoLog: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, logs := bufferLogger()
			observer := &recordingObserver{}
			relay := New(tt.store, failingPublisher{}, func(s string) string { return s },
				Config{Retention: time.Hour}, logger).WithObserver(observer)
			relay.now = func() time.Time { return now }

			relay.maintain(t.Context())

			require.Equal(t, tt.wantBacklog, observer.backlogs)
			require.Len(t, tt.store.pruneCutoffs, tt.wantPrunes)

			if tt.wantPrunes > 0 {
				require.Equal(t, now.UTC().Add(-time.Hour), tt.store.pruneCutoffs[0])
			}

			if tt.wantNoLog {
				require.Empty(t, logs.String())
			} else {
				require.Contains(t, logs.String(), tt.wantLog)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	for attempt, want := range map[int]time.Duration{
		1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 7: 60 * time.Second, 40: 60 * time.Second,
	} {
		require.Equal(t, want, Backoff(attempt, time.Second, time.Minute), "attempt %d", attempt)
	}
}
