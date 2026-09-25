package service

import (
	"context"
	"fmt"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

const (
	// retentionBatch is how many rows of each raw table one delete removes.
	retentionBatch = 5000
	// retentionMaxBatches bounds one run; the next run carries on.
	retentionMaxBatches = 200
)

// Retention deletes raw events and daily rollups past their retention. Weekly and monthly
// rollups, cohorts, and first-seen times are kept forever.
type Retention struct {
	repo     ports.RetentionRepository
	settings ports.RetentionSettings
	tz       ports.TimezoneSource
	clock    Clock
}

// NewRetention returns a Retention reading its periods from settings.
func NewRetention(repo ports.RetentionRepository, settings ports.RetentionSettings, tz ports.TimezoneSource, clock Clock) *Retention {
	return &Retention{repo: repo, settings: settings, tz: tz, clock: clock}
}

// RetentionResult reports what a run deleted. RollupsBefore is empty when daily rollups are
// kept forever.
type RetentionResult struct {
	RawBefore      time.Time
	RawDeleted     int64
	RollupsBefore  string
	RollupsDeleted int64
}

// Run deletes raw events older than analytics.raw_retention_days (but never those the
// aggregator may still recompute from) and daily rollups older than
// analytics.rollup_day_retention_months.
func (r *Retention) Run(ctx context.Context) (RetentionResult, error) {
	var result RetentionResult

	name, err := r.tz.Timezone(ctx)
	if err != nil {
		return result, err
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return result, fmt.Errorf("site time zone %q: %w", name, err)
	}

	days, err := r.settings.RawRetentionDays(ctx)
	if err != nil {
		return result, err
	}

	months, err := r.settings.DayRollupRetentionMonths(ctx)
	if err != nil {
		return result, err
	}

	now := r.clock.Now()
	result.RawBefore = domain.RawRetentionCutoff(now, loc, days)

	for range retentionMaxBatches {
		deleted, err := r.repo.PruneRaw(ctx, result.RawBefore, retentionBatch)
		if err != nil {
			return result, err
		}

		result.RawDeleted += deleted
		if deleted == 0 {
			break
		}
	}

	day, ok := domain.DayRollupCutoff(now, loc, months)
	if !ok {
		return result, nil
	}

	result.RollupsBefore = day
	result.RollupsDeleted, err = r.repo.PruneDayRollups(ctx, day)

	return result, err
}
