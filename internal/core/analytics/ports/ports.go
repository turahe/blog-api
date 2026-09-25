// Package ports defines the analytics module's outbound interfaces.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
)

// Sink accepts events for asynchronous storage. Enqueue never blocks; it reports false when
// the event was dropped.
type Sink interface {
	Enqueue(event domain.Event) bool
}

// LiveSink announces accepted events to the live view of every API replica. Publish never
// blocks; a busy sink drops events.
type LiveSink interface {
	Publish(event domain.LiveEvent)
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

// ReportRepository reads rollups for the admin dashboard. Lists are ordered by their main
// measure, largest first.
type ReportRepository interface {
	SiteRows(ctx context.Context, sel domain.Selection) ([]domain.SiteRow, error)
	Referrers(ctx context.Context, sel domain.Selection, limit int) ([]domain.ReferrerRow, error)
	Dimensions(ctx context.Context, sel domain.Selection) ([]domain.DimensionRow, error)
	// Pages orders by domain.SortViews, SortTime (average focus), or SortRising (views gained
	// over prev); the latter two leave out the folded remainder.
	Pages(ctx context.Context, cur, prev domain.Selection, sort string, limit int) ([]domain.PageRow, error)
	Transitions(ctx context.Context, sel domain.Selection, limit int) ([]domain.TransitionRow, error)
	EntryPages(ctx context.Context, sel domain.Selection, limit int) ([]domain.PathCount, error)
	ExitPages(ctx context.Context, sel domain.Selection, limit int) ([]domain.PathCount, error)
	// Queries orders by searches, or by zero-result searches (leaving out the remainder)
	// when zeroResults is set.
	Queries(ctx context.Context, sel domain.Selection, zeroResults bool, limit int) ([]domain.QueryRow, error)
	// QueryTotals sums every query row, including the folded remainder.
	QueryTotals(ctx context.Context, sel domain.Selection) (domain.QueryRow, error)
	Positions(ctx context.Context, sel domain.Selection, limit int) ([]domain.PositionRow, error)
	ClickedResults(ctx context.Context, sel domain.Selection, limit int) ([]domain.ResultRow, error)
	// Cohorts returns the cohorts of the local days first through last, oldest first.
	Cohorts(ctx context.Context, first, last string) ([]domain.CohortRow, error)
}

// TimezoneSource returns the site time zone rollups are bucketed in.
type TimezoneSource interface {
	Timezone(ctx context.Context) (string, error)
}

// ExportRepository stores rollup export requests.
type ExportRepository interface {
	// Create inserts a pending export; a second open export of the same user is
	// domain.ErrExportOpen.
	Create(ctx context.Context, export domain.Export) error
	// Get returns the user's export, or domain.ErrExportNotFound.
	Get(ctx context.Context, id, userID uuid.UUID) (domain.Export, error)
	// Open returns the user's pending or running export, or domain.ErrExportNotFound.
	Open(ctx context.Context, userID uuid.UUID) (domain.Export, error)
	// List returns the user's most recent exports, newest first.
	List(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Export, error)
	// ClaimNext marks the oldest pending export, or one running since before staleBefore,
	// running.
	ClaimNext(ctx context.Context, now, staleBefore time.Time) (domain.Export, bool, error)
	Complete(ctx context.Context, id uuid.UUID, storageKey string, size int64, expiresAt, at time.Time) error
	// Fail records message and requeues the export, or marks it failed when final.
	Fail(ctx context.Context, id uuid.UUID, message string, final bool, at time.Time) error
	// Archives returns exports whose archive expired before before, oldest first.
	Archives(ctx context.Context, before time.Time, limit int) ([]domain.Export, error)
	// ClearArchive forgets a deleted archive.
	ClearArchive(ctx context.Context, id uuid.UUID) error
}

// ArchiveStore keeps export archives in object storage.
type ArchiveStore interface {
	PutObject(ctx context.Context, key, contentType string, body []byte) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	DeleteObject(ctx context.Context, key string) error
}

// TableWriter receives exported tables: Table starts one, and Row adds a row to it.
type TableWriter interface {
	Table(name string, columns []string) error
	Row(values []string) error
}

// RollupExporter reads rollups as tables of text values; NULL is an empty string.
type RollupExporter interface {
	// ExportRollups writes every rollup table's rows of sel, and the cohorts of the local days
	// first through last.
	ExportRollups(ctx context.Context, sel domain.Selection, first, last string, w TableWriter) error
}

// StepUp re-verifies the current password, and the two-factor code when the user has two-factor
// authentication enabled.
type StepUp interface {
	VerifyStepUp(ctx context.Context, userID uuid.UUID, password, code string) (bool, error)
}

// RetentionRepository deletes expired analytics data.
type RetentionRepository interface {
	// PruneRaw deletes up to limit raw events of each raw table stored before before,
	// returning how many rows it deleted.
	PruneRaw(ctx context.Context, before time.Time, limit int) (int64, error)
	// PruneDayRollups deletes daily rollups of periods starting before the local date day.
	PruneDayRollups(ctx context.Context, day string) (int64, error)
}

// RetentionSettings returns how long analytics data is kept.
type RetentionSettings interface {
	// RawRetentionDays is how many days raw events are kept.
	RawRetentionDays(ctx context.Context) (int, error)
	// DayRollupRetentionMonths is how many months daily rollups are kept; 0 keeps them forever.
	DayRollupRetentionMonths(ctx context.Context) (int, error)
}
