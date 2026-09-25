// Package realtime fans notifications out to the live SSE connections of this process.
package realtime

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	notificationdomain "github.com/turahe/blog-api/internal/core/notification/domain"
)

var (
	// ErrTooManyConnections is returned when the user already holds the maximum number of streams.
	ErrTooManyConnections = errors.New("too many notification streams for this user")
	// ErrClosed is returned after Shutdown.
	ErrClosed = errors.New("notification hub is shut down")
)

// Hub routes each notification to its recipient's connections. A full connection buffer
// drops the event for that connection only; the hub never waits on a slow client.
type Hub struct {
	mu         sync.Mutex
	conns      map[uuid.UUID]map[*Conn]struct{}
	maxPerUser int
	buffer     int
	closed     bool
}

// Conn is one registered stream.
type Conn struct {
	events   chan notificationdomain.Notification
	done     chan struct{}
	dropped  atomic.Int64
	userID   uuid.UUID
	stopOnce sync.Once
}

// NewHub returns a Hub allowing maxPerUser streams per user, each buffering buffer events.
func NewHub(maxPerUser, buffer int) *Hub {
	return &Hub{
		conns:      map[uuid.UUID]map[*Conn]struct{}{},
		maxPerUser: max(maxPerUser, 1),
		buffer:     max(buffer, 1),
	}
}

// Register opens a stream for userID.
func (h *Hub) Register(userID uuid.UUID) (*Conn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, ErrClosed
	}

	if len(h.conns[userID]) >= h.maxPerUser {
		return nil, ErrTooManyConnections
	}

	conn := &Conn{
		events: make(chan notificationdomain.Notification, h.buffer),
		done:   make(chan struct{}),
		userID: userID,
	}

	if h.conns[userID] == nil {
		h.conns[userID] = map[*Conn]struct{}{}
	}

	h.conns[userID][conn] = struct{}{}

	return conn, nil
}

// Unregister removes a stream; it is safe to call more than once.
func (h *Hub) Unregister(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.conns[conn.userID], conn)

	if len(h.conns[conn.userID]) == 0 {
		delete(h.conns, conn.userID)
	}
}

// Deliver queues n on every stream of its recipient.
func (h *Hub) Deliver(n notificationdomain.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.conns[n.UserUUID] {
		select {
		case conn.events <- n:
		default:
			conn.dropped.Add(1)
		}
	}
}

// Connections returns the number of open streams.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	total := 0
	for _, conns := range h.conns {
		total += len(conns)
	}

	return total
}

// Shutdown refuses new streams and tells every open stream to close.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true

	for _, conns := range h.conns {
		for conn := range conns {
			conn.stopOnce.Do(func() { close(conn.done) })
		}
	}
}

// Events yields the notifications queued for this stream.
func (c *Conn) Events() <-chan notificationdomain.Notification {
	return c.events
}

// Done is closed when the hub shuts down.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

// TakeDropped returns how many events were dropped since the last call and resets the count.
func (c *Conn) TakeDropped() int64 {
	return c.dropped.Swap(0)
}
