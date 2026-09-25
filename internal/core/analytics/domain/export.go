package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// PermExport lets a user export rollups. It is admin only.
const PermExport = "analytics.export"

// Export errors.
var (
	ErrExportNotFound    = errors.New("analytics: export not found")
	ErrExportOpen        = errors.New("analytics: an export is already in progress")
	ErrExportUnavailable = errors.New("analytics: exports need object storage")
	ErrStepUpRequired    = errors.New("analytics: step-up verification failed")
)

// ExportStatus is where an export is in its lifecycle.
type ExportStatus string

// Export statuses.
const (
	ExportPending   ExportStatus = "pending"
	ExportRunning   ExportStatus = "running"
	ExportCompleted ExportStatus = "completed"
	ExportFailed    ExportStatus = "failed"
)

// Export is a requested ZIP of rollup CSVs. FirstDay and LastDay are the whole periods it
// covers, read in Timezone.
type Export struct {
	UUID        uuid.UUID
	RequestedBy uuid.UUID
	Grain       Grain
	FirstDay    string
	LastDay     string
	Timezone    string
	Status      ExportStatus
	StorageKey  *string
	SizeBytes   *int64
	ExpiresAt   *time.Time
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// Open reports whether the export is still queued or being built.
func (e Export) Open() bool {
	return e.Status == ExportPending || e.Status == ExportRunning
}

// Downloadable reports whether the archive can still be fetched at now.
func (e Export) Downloadable(now time.Time) bool {
	return e.Status == ExportCompleted && e.StorageKey != nil && e.ExpiresAt != nil && now.Before(*e.ExpiresAt)
}

// ExportExpired is the state of a completed export whose archive is gone.
const ExportExpired = "expired"

// State is the status shown to the requester: a completed export past its retention is
// ExportExpired.
func (e Export) State(now time.Time) string {
	if e.Status == ExportCompleted && !e.Downloadable(now) {
		return ExportExpired
	}

	return string(e.Status)
}

// Window returns the periods the export covers.
func (e Export) Window() (Window, error) {
	loc, err := time.LoadLocation(e.Timezone)
	if err != nil {
		return Window{}, err
	}

	first, err := time.ParseInLocation(time.DateOnly, e.FirstDay, loc)
	if err != nil {
		return Window{}, err
	}

	last, err := time.ParseInLocation(time.DateOnly, e.LastDay, loc)
	if err != nil {
		return Window{}, err
	}

	return Window{Grain: e.Grain, Periods: PeriodsBetween(e.Grain, first, last, loc)}, nil
}

// RawRetentionFloorDays is the fewest days of raw events retention keeps, whatever the setting:
// the aggregator still recomputes cohorts that far back.
const RawRetentionFloorDays = 31

// RawRetentionCutoff returns the instant before which raw events may be deleted when keeping
// days days, at now in loc. It never reaches into the previous calendar month, which the
// aggregator recomputes on the first of the month, or the last RawRetentionFloorDays days.
func RawRetentionCutoff(now time.Time, loc *time.Location, days int) time.Time {
	today := localMidnight(now, loc)
	cutoff := today.AddDate(0, 0, -max(days, RawRetentionFloorDays))

	if previousMonth := time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, loc); previousMonth.Before(cutoff) {
		cutoff = previousMonth
	}

	return cutoff
}

// DayRollupCutoff returns the first local date whose daily rollups are kept when keeping
// months months, at now in loc; ok is false when months is 0 (keep forever).
func DayRollupCutoff(now time.Time, loc *time.Location, months int) (string, bool) {
	if months <= 0 {
		return "", false
	}

	return localMidnight(now, loc).AddDate(0, -months, 0).Format(time.DateOnly), true
}

func localMidnight(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.In(loc).Date()

	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}
