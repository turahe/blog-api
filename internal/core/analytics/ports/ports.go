// Package ports defines the analytics module's outbound interfaces.
package ports

import (
	"context"
	"time"

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

// RollupRepository builds rollups from raw events. Every method is idempotent.
type RollupRepository interface {
	// EarliestEvent returns the oldest stored raw page view or search time.
	EarliestEvent(ctx context.Context) (time.Time, bool, error)
	// Timezone returns the time zone the rollups were built in.
	Timezone(ctx context.Context) (string, bool, error)
	SetTimezone(ctx context.Context, timezone string, now time.Time) error
	// DeleteFrom removes every period and cohort starting on or after the local date day.
	DeleteFrom(ctx context.Context, day string) error
	// RefreshFirstSeen records the first page view of subjects seen in [from, to).
	RefreshFirstSeen(ctx context.Context, from, to time.Time) error
	// RecomputePeriod replaces every rollup row of the period.
	RecomputePeriod(ctx context.Context, period domain.Period, topN int, now time.Time) error
	// RecomputeCohort replaces the cohort row.
	RecomputeCohort(ctx context.Context, cohort domain.Cohort, now time.Time) error
}

// TimezoneSource returns the site time zone rollups are bucketed in.
type TimezoneSource interface {
	Timezone(ctx context.Context) (string, error)
}
