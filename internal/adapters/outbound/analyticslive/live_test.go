package analyticslive

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 25, 10, 0, 0, 123e6, time.UTC)
	events := []domain.LiveEvent{
		{Kind: domain.KindPageView, At: at, Session: uuid.New(), Path: "/a", Country: "DE", Device: domain.DeviceMobile},
		{Kind: domain.KindSearch, At: at, Session: uuid.New(), Query: "go", Results: 4},
		{Kind: domain.KindTimeSpent, At: at, Session: uuid.New()},
	}

	payload, err := Encode(events)
	require.NoError(t, err)

	decoded, err := Decode(payload)
	require.NoError(t, err)
	assert.Equal(t, events, decoded)
}

func TestDecodeSkipsInvalidEvents(t *testing.T) {
	t.Parallel()

	decoded, err := Decode([]byte(`{"events":[{"k":"page_view","t":1,"s":"00000000-0000-0000-0000-000000000000"},
		{"k":"unknown","t":1,"s":"` + uuid.NewString() + `"},{"k":"search","t":1,"s":"` + uuid.NewString() + `","q":"x"}]}`))
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	assert.Equal(t, "x", decoded[0].Query)

	_, err = Decode([]byte(`not json`))
	require.Error(t, err)
}

type collected struct {
	mu     sync.Mutex
	events []domain.LiveEvent
}

func (c *collected) add(events ...domain.LiveEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, events...)
}

func (c *collected) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.events)
}

func TestPublisherBatchesToConsumers(t *testing.T) {
	t.Parallel()

	pubsub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})

	t.Cleanup(func() { _ = pubsub.Close() })

	got := &collected{}
	require.NoError(t, Consume(t.Context(), pubsub, Topic, got.add, nil))

	publisher := NewPublisher(pubsub, Topic, nil)
	go publisher.Run(t.Context(), 10*time.Millisecond)

	for range 3 {
		publisher.Publish(domain.LiveEvent{Kind: domain.KindPageView, At: time.Now(), Session: uuid.New(), Path: "/a"})
	}

	require.Eventually(t, func() bool { return got.count() == 3 }, 2*time.Second, 10*time.Millisecond)
}

func TestPublisherDropsWhenTheQueueIsFull(t *testing.T) {
	t.Parallel()

	publisher := NewPublisher(nil, Topic, nil)
	for range queueSize + 2 {
		publisher.Publish(domain.LiveEvent{Kind: domain.KindPageView, Session: uuid.New()})
	}

	assert.Equal(t, int64(2), publisher.dropped.Load())
}

// lockedBuffer is a log sink safe to read while the publisher goroutine writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

type recordingPublisher struct {
	mu      sync.Mutex
	batches [][]byte
	err     error
}

func (p *recordingPublisher) Publish(_ string, msgs ...*message.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, msg := range msgs {
		p.batches = append(p.batches, msg.Payload)
	}

	return p.err
}

func (p *recordingPublisher) Close() error { return nil }

func (p *recordingPublisher) sizes(t *testing.T) []int {
	t.Helper()

	p.mu.Lock()
	defer p.mu.Unlock()

	sizes := make([]int, 0, len(p.batches))

	for _, payload := range p.batches {
		events, err := Decode(payload)
		require.NoError(t, err)

		sizes = append(sizes, len(events))
	}

	return sizes
}

func pageView() domain.LiveEvent {
	return domain.LiveEvent{Kind: domain.KindPageView, At: time.Now(), Session: uuid.New(), Path: "/a"}
}

// runPublisher starts Run inside the current synctest bubble and stops it at cleanup.
func runPublisher(t *testing.T, publisher *Publisher) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)
		publisher.Run(ctx, FlushEvery)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestPublisherRunFlushesOnTick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pub := &recordingPublisher{}
		publisher := NewPublisher(pub, Topic, slog.New(slog.DiscardHandler))

		for range 3 {
			publisher.Publish(pageView())
		}

		runPublisher(t, publisher)

		synctest.Wait()
		assert.Empty(t, pub.sizes(t), "nothing is published before the first tick")

		time.Sleep(FlushEvery)
		synctest.Wait()
		assert.Equal(t, []int{3}, pub.sizes(t))

		time.Sleep(FlushEvery)
		synctest.Wait()
		assert.Equal(t, []int{3}, pub.sizes(t), "an empty tick publishes nothing")
	})
}

func TestPublisherRunFlushesFullBatchesAndReportsDrops(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pub := &recordingPublisher{}
		logs := &lockedBuffer{}
		publisher := NewPublisher(pub, Topic, slog.New(slog.NewTextHandler(logs, nil)))

		for range batchSize + 1 {
			publisher.Publish(pageView())
		}

		publisher.dropped.Add(2)

		runPublisher(t, publisher)
		synctest.Wait()

		assert.Equal(t, []int{batchSize}, pub.sizes(t), "a full batch is published without waiting for the tick")
		assert.Contains(t, logs.String(), "queue full, events dropped")
		assert.Contains(t, logs.String(), "dropped=2")
		assert.Zero(t, publisher.dropped.Load())
	})
}

func TestPublisherRunLogsPublishFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pub := &recordingPublisher{err: errors.New("broker down")}
		logs := &lockedBuffer{}
		publisher := NewPublisher(pub, Topic, slog.New(slog.NewTextHandler(logs, nil)))
		publisher.Publish(pageView())

		runPublisher(t, publisher)

		time.Sleep(FlushEvery)
		synctest.Wait()

		assert.Contains(t, logs.String(), "batch not published")
	})
}

type fakeSubscriber struct {
	messages chan *message.Message
	err      error
}

func (s fakeSubscriber) Subscribe(context.Context, string) (<-chan *message.Message, error) {
	return s.messages, s.err
}

func (s fakeSubscriber) Close() error { return nil }

func TestConsumeReportsSubscribeFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	err := Consume(t.Context(), fakeSubscriber{err: boom}, Topic, func(...domain.LiveEvent) {
		t.Error("deliver must not be called")
	}, nil)
	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, Topic)
}

func TestConsumeAcksWithoutDelivering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		wantLog string
	}{
		{name: "malformed batch", payload: "garbage", wantLog: "dropped malformed batch"},
		{name: "batch without valid events", payload: `{"events":[{"k":"unknown"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			messages := make(chan *message.Message, 1)
			t.Cleanup(func() { close(messages) })

			logs := &lockedBuffer{}
			got := &collected{}

			require.NoError(t, Consume(t.Context(), fakeSubscriber{messages: messages}, Topic, got.add,
				slog.New(slog.NewTextHandler(logs, nil))))

			msg := message.NewMessage(watermill.NewUUID(), []byte(tt.payload))
			messages <- msg

			select {
			case <-msg.Acked():
			case <-time.After(5 * time.Second):
				t.Fatal("message was not acknowledged")
			}

			assert.Zero(t, got.count())

			if tt.wantLog != "" {
				assert.Contains(t, logs.String(), tt.wantLog)
			} else {
				assert.Empty(t, logs.String())
			}
		})
	}
}
