// Package ports defines the analytics module's outbound interfaces.
package ports

import (
	"context"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

// Sink accepts events for asynchronous storage. Enqueue never blocks; it reports false when
// the event was dropped.
type Sink interface {
	Enqueue(event domain.Event) bool
}

// EventRepository stores raw events.
type EventRepository interface {
	// InsertBatch stores events, ignoring ones whose UUID is already stored; a time-spent
	// event for a stored page view raises its focus time instead.
	InsertBatch(ctx context.Context, events []domain.Event) error
}

// IdentityHasher keys visitor hashes (HMAC-SHA256 under APP_ENCRYPTION_KEY).
type IdentityHasher interface {
	MAC(value string) string
}
