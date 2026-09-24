package messaging

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
)

type txTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *txTransport) Flush(time.Duration) bool              { return true }
func (t *txTransport) FlushWithContext(context.Context) bool { return true }
func (t *txTransport) Configure(sentry.ClientOptions)        {}
func (t *txTransport) Close()                                {}

func (t *txTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.events = append(t.events, event)
}

//nolint:paralleltest // binds the global Sentry hub
func TestTracingRecordsTransactionPerMessage(t *testing.T) {
	transport := &txTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:              "https://public@example.com/1",
		EnableTracing:    true,
		TracesSampleRate: 1,
		Transport:        transport,
	}))
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	wantErr := errors.New("handler failed")

	var handlerHub *sentry.Hub

	handler := Tracing(func(msg *message.Message) ([]*message.Message, error) {
		handlerHub = sentry.GetHubFromContext(msg.Context())
		return nil, wantErr
	})

	_, err := handler(message.NewMessage("id-1", nil))
	require.ErrorIs(t, err, wantErr)
	require.NotNil(t, handlerHub)
	require.NotSame(t, sentry.CurrentHub(), handlerHub)

	transport.mu.Lock()
	defer transport.mu.Unlock()

	require.Len(t, transport.events, 1)
	require.Equal(t, "transaction", transport.events[0].Type)
	require.Equal(t, "queue.process", transport.events[0].Contexts["trace"]["op"])
}
