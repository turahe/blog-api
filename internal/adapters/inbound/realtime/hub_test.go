package realtime

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

func TestHubRoutesToRecipientOnly(t *testing.T) {
	t.Parallel()

	hub := NewHub(3, 4)
	alice, bob := uuid.New(), uuid.New()

	a1, err := hub.Register(alice)
	require.NoError(t, err)
	a2, err := hub.Register(alice)
	require.NoError(t, err)
	b, err := hub.Register(bob)
	require.NoError(t, err)

	n := notificationdomain.Notification{UUID: uuid.New(), UserUUID: alice}
	hub.Deliver(n)

	require.Equal(t, n, <-a1.Events())
	require.Equal(t, n, <-a2.Events(), "every stream of the user gets the event")
	require.Empty(t, b.Events(), "other users never see it")
}

func TestHubLimitsStreamsPerUser(t *testing.T) {
	t.Parallel()

	hub := NewHub(2, 1)
	user := uuid.New()

	first, err := hub.Register(user)
	require.NoError(t, err)
	_, err = hub.Register(user)
	require.NoError(t, err)

	_, err = hub.Register(user)
	require.ErrorIs(t, err, ErrTooManyConnections)

	hub.Unregister(first)
	hub.Unregister(first)

	_, err = hub.Register(user)
	require.NoError(t, err, "closing a stream frees its slot")
	require.Equal(t, 2, hub.Connections())
}

func TestHubDropsForFullBuffer(t *testing.T) {
	t.Parallel()

	hub := NewHub(1, 2)
	user := uuid.New()
	conn, err := hub.Register(user)
	require.NoError(t, err)

	for range 5 {
		hub.Deliver(notificationdomain.Notification{UUID: uuid.New(), UserUUID: user})
	}

	require.Len(t, conn.Events(), 2)
	require.Equal(t, int64(3), conn.TakeDropped())
	require.Zero(t, conn.TakeDropped(), "the count resets once read")
}

func TestHubShutdown(t *testing.T) {
	t.Parallel()

	hub := NewHub(1, 1)
	conn, err := hub.Register(uuid.New())
	require.NoError(t, err)

	hub.Shutdown()
	hub.Shutdown()

	select {
	case <-conn.Done():
	default:
		t.Fatal("open streams are told to close")
	}

	_, err = hub.Register(uuid.New())
	require.ErrorIs(t, err, ErrClosed)
}
