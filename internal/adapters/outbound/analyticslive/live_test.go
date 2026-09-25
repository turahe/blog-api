package analyticslive

import (
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
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
