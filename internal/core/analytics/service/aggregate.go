package service

import (
	"context"
	"fmt"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

// cohortLookback is how many days back a run recomputes cohorts: a cohort's day-30 return
// changes until the 30th day after it has ended.
const cohortLookback = 30

// Aggregator builds rollups from raw events in the site time zone.
type Aggregator struct {
	repo  ports.RollupRepository
	tz    ports.TimezoneSource
	clock Clock
	topN  int
}

// NewAggregator returns an Aggregator keeping domain.RollupTopN values per capped dimension.
func NewAggregator(repo ports.RollupRepository, tz ports.TimezoneSource, clock Clock) *Aggregator {
	return &Aggregator{repo: repo, tz: tz, clock: clock, topN: domain.RollupTopN}
}

// AggregateResult reports what a run recomputed.
type AggregateResult struct {
	Periods int
	Cohorts int
	// Rebuilt is true when the run rebuilt every period the raw events cover: on the first run,
	// or after site.timezone changed.
	Rebuilt bool
}

// window is what one recompute covers: the periods overlapping the local days from through
// to, starting no earlier than minStart when set, and the cohorts of cohortFrom through to.
type window struct {
	from, to, cohortFrom, minStart time.Time
}

// Run recomputes the periods containing today or yesterday and the cohorts whose returns may
// still change. Events are timed on receipt, so nothing lands in an older period; yesterday
// covers events still buffered at midnight.
func (a *Aggregator) Run(ctx context.Context) (AggregateResult, error) {
	loc, result, rebuilt, err := a.prepare(ctx)
	if err != nil || rebuilt {
		return result, err
	}

	now := a.clock.Now()
	yesterday := now.In(loc).AddDate(0, 0, -1)

	return a.recompute(ctx, loc, window{from: yesterday, to: now, cohortFrom: yesterday.AddDate(0, 0, -cohortLookback)}, now)
}

// Backfill recomputes the calendar days from through to (their dates are read in the site
// time zone) and the cohorts that can still change within them. Days before the oldest raw
// event and after today are skipped: their raw events are gone or not yet there.
func (a *Aggregator) Backfill(ctx context.Context, from, to time.Time) (AggregateResult, error) {
	loc, result, rebuilt, err := a.prepare(ctx)
	if err != nil || rebuilt {
		return result, err
	}

	earliest, ok, err := a.repo.EarliestEvent(ctx)
	if err != nil || !ok {
		return AggregateResult{}, err
	}

	now := a.clock.Now()
	first := domain.PeriodOf(domain.GrainDay, earliest, loc).Start
	from = later(localDate(from, loc), first)
	to = earlier(localDate(to, loc), now.In(loc))

	if from.After(to) {
		return AggregateResult{}, nil
	}

	return a.recompute(ctx, loc, window{from: from, to: to, cohortFrom: later(from.AddDate(0, 0, -cohortLookback), first)}, now)
}

// prepare loads the site time zone and rebuilds the rollups when they were built in another
// one, or never.
func (a *Aggregator) prepare(ctx context.Context) (*time.Location, AggregateResult, bool, error) {
	name, err := a.tz.Timezone(ctx)
	if err != nil {
		return nil, AggregateResult{}, false, err
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, AggregateResult{}, false, fmt.Errorf("site time zone %q: %w", name, err)
	}

	stored, found, err := a.repo.Timezone(ctx)
	if err != nil || (found && stored == name) {
		return loc, AggregateResult{}, false, err
	}

	result, err := a.rebuild(ctx, loc, name, !found)

	return loc, result, true, err
}

// rebuild recomputes every period and cohort the raw events cover. After a time zone change
// it only replaces periods starting on or after the oldest raw event's day: older ones, whose
// raw events are gone, keep the boundaries of the old zone.
func (a *Aggregator) rebuild(ctx context.Context, loc *time.Location, name string, first bool) (AggregateResult, error) {
	now := a.clock.Now()

	earliest, ok, err := a.repo.EarliestEvent(ctx)
	if err != nil {
		return AggregateResult{}, err
	}

	result := AggregateResult{Rebuilt: true}

	if ok {
		start := domain.PeriodOf(domain.GrainDay, earliest, loc)
		w := window{from: start.Start, to: now, cohortFrom: start.Start}

		if !first {
			if err := a.repo.DeleteFrom(ctx, start.Day()); err != nil {
				return result, err
			}

			w.minStart = start.Start
		}

		recomputed, err := a.recompute(ctx, loc, w, now)
		if err != nil {
			return result, err
		}

		result.Periods, result.Cohorts = recomputed.Periods, recomputed.Cohorts
	}

	return result, a.repo.SetTimezone(ctx, name, now)
}

func (a *Aggregator) recompute(ctx context.Context, loc *time.Location, w window, now time.Time) (AggregateResult, error) {
	var result AggregateResult

	days := domain.PeriodsBetween(domain.GrainDay, w.from, w.to, loc)
	if len(days) == 0 {
		return result, nil
	}

	if err := a.repo.RefreshFirstSeen(ctx, days[0].From, days[len(days)-1].To); err != nil {
		return result, err
	}

	for _, grain := range domain.Grains {
		for _, period := range domain.PeriodsBetween(grain, w.from, w.to, loc) {
			if period.Start.Before(w.minStart) {
				continue
			}

			if err := a.repo.RecomputePeriod(ctx, period, a.topN, now); err != nil {
				return result, fmt.Errorf("recompute %s %s: %w", grain, period.Day(), err)
			}

			result.Periods++
		}
	}

	for _, day := range domain.PeriodsBetween(domain.GrainDay, later(w.cohortFrom, w.minStart), w.to, loc) {
		if err := a.repo.RecomputeCohort(ctx, domain.CohortOf(day.Start, loc, now), now); err != nil {
			return result, fmt.Errorf("recompute cohort %s: %w", day.Day(), err)
		}

		result.Cohorts++
	}

	return result, nil
}

// localDate returns midnight in loc of t's calendar date, whatever t's own location.
func localDate(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.Date()

	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}
