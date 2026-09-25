package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
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

func TestBackoff(t *testing.T) {
	t.Parallel()

	for attempt, want := range map[int]time.Duration{
		1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 7: 60 * time.Second, 40: 60 * time.Second,
	} {
		require.Equal(t, want, Backoff(attempt, time.Second, time.Minute), "attempt %d", attempt)
	}
}
