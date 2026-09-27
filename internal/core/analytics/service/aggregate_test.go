package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

type fakeRollups struct {
	earliest    time.Time
	hasEvents   bool
	timezone    string
	hasTimezone bool
	deletedFrom []string
	firstSeen   [][2]time.Time
	periods     []string
	cohorts     []string
	failPeriod  error
	earliestErr error
	timezoneErr error
	deleteErr   error
	refreshErr  error
	cohortErr   error
}

func (f *fakeRollups) EarliestEvent(context.Context) (time.Time, bool, error) {
	return f.earliest, f.hasEvents, f.earliestErr
}

func (f *fakeRollups) Timezone(context.Context) (string, bool, error) {
	return f.timezone, f.hasTimezone, f.timezoneErr
}

func (f *fakeRollups) SetTimezone(_ context.Context, timezone string, _ time.Time) error {
	f.timezone, f.hasTimezone = timezone, true

	return nil
}

func (f *fakeRollups) DeleteFrom(_ context.Context, day string) error {
	f.deletedFrom = append(f.deletedFrom, day)

	return f.deleteErr
}

func (f *fakeRollups) RefreshFirstSeen(_ context.Context, from, to time.Time) error {
	f.firstSeen = append(f.firstSeen, [2]time.Time{from, to})

	return f.refreshErr
}

func (f *fakeRollups) RecomputePeriod(_ context.Context, period domain.Period, _ int, _ time.Time) error {
	f.periods = append(f.periods, string(period.Grain)+" "+period.Day())

	return f.failPeriod
}

func (f *fakeRollups) RecomputeCohort(_ context.Context, cohort domain.Cohort, _ time.Time) error {
	f.cohorts = append(f.cohorts, cohort.Day.Day())

	return f.cohortErr
}

type staticZone string

func (z staticZone) Timezone(context.Context) (string, error) { return string(z), nil }

type failingZone struct{ err error }

func (z failingZone) Timezone(context.Context) (string, error) { return "", z.err }

// Friday 2026-09-25, noon UTC.
var aggregateNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func newTestAggregator(repo *fakeRollups, zone string) *Aggregator {
	return NewAggregator(repo, staticZone(zone), &fixedClock{now: aggregateNow})
}

func TestAggregatorRunRefreshesOpenPeriods(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{timezone: "UTC", hasTimezone: true, hasEvents: true, earliest: aggregateNow.AddDate(0, -3, 0)}

	result, err := newTestAggregator(repo, "UTC").Run(t.Context())
	require.NoError(t, err)

	assert.False(t, result.Rebuilt)
	assert.Equal(t, []string{"day 2026-09-24", "day 2026-09-25", "week 2026-09-21", "month 2026-09-01"}, repo.periods)
	assert.Equal(t, 4, result.Periods)
	assert.Len(t, repo.cohorts, 32, "yesterday and the 30 days before it, plus today")
	assert.Equal(t, "2026-08-25", repo.cohorts[0])
	assert.Equal(t, [][2]time.Time{{
		time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
	}}, repo.firstSeen)
	assert.Empty(t, repo.deletedFrom)
}

func TestAggregatorRunIncludesTheClosingMonthOnItsFirstDay(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{timezone: "UTC", hasTimezone: true}
	agg := NewAggregator(repo, staticZone("UTC"), &fixedClock{now: time.Date(2026, 10, 1, 0, 5, 0, 0, time.UTC)})

	_, err := agg.Run(t.Context())
	require.NoError(t, err)
	assert.Contains(t, repo.periods, "month 2026-09-01", "events buffered over midnight still reach September")
	assert.Contains(t, repo.periods, "month 2026-10-01")
}

func TestAggregatorFirstRunBuildsEverything(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{hasEvents: true, earliest: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)}

	result, err := newTestAggregator(repo, "UTC").Run(t.Context())
	require.NoError(t, err)

	assert.True(t, result.Rebuilt)
	assert.Equal(t, "UTC", repo.timezone)
	assert.Empty(t, repo.deletedFrom, "there is nothing to replace on the first run")
	assert.Equal(t, []string{
		"day 2026-09-23", "day 2026-09-24", "day 2026-09-25", "week 2026-09-21", "month 2026-09-01",
	}, repo.periods)
	assert.Equal(t, []string{"2026-09-23", "2026-09-24", "2026-09-25"}, repo.cohorts)
}

func TestAggregatorFirstRunWithoutEventsOnlyRecordsTheZone(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{}

	result, err := newTestAggregator(repo, "Asia/Jakarta").Run(t.Context())
	require.NoError(t, err)
	assert.True(t, result.Rebuilt)
	assert.Equal(t, "Asia/Jakarta", repo.timezone)
	assert.Empty(t, repo.periods)
}

func TestAggregatorRebuildsAfterATimezoneChange(t *testing.T) {
	t.Parallel()

	// Wednesday 2026-09-23 in Jakarta: the week (from Monday) and the month started before the
	// oldest raw event and keep their old rows.
	repo := &fakeRollups{
		timezone: "UTC", hasTimezone: true, hasEvents: true, earliest: time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
	}

	result, err := newTestAggregator(repo, "Asia/Jakarta").Run(t.Context())
	require.NoError(t, err)

	assert.True(t, result.Rebuilt)
	assert.Equal(t, []string{"2026-09-23"}, repo.deletedFrom)
	assert.Equal(t, []string{"day 2026-09-23", "day 2026-09-24", "day 2026-09-25"}, repo.periods)
	assert.Equal(t, "Asia/Jakarta", repo.timezone)
}

func TestAggregatorFirstRunWithOnlyFutureEventsBuildsNothing(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{hasEvents: true, earliest: aggregateNow.Add(48 * time.Hour)}

	result, err := newTestAggregator(repo, "UTC").Run(t.Context())
	require.NoError(t, err)
	assert.True(t, result.Rebuilt)
	assert.Empty(t, repo.firstSeen)
	assert.Empty(t, repo.periods)
	assert.Equal(t, "UTC", repo.timezone)
}

func TestAggregatorBackfillRebuildsOnTheFirstRun(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{hasEvents: true, earliest: time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)}

	result, err := newTestAggregator(repo, "UTC").Backfill(t.Context(),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, result.Rebuilt, "the rebuild covers every stored event instead")
	assert.Contains(t, repo.periods, "day 2026-09-24")
	assert.NotContains(t, repo.periods, "day 2026-01-01")
}

func TestAggregatorBackfillSkipsWhenNothingIsStored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo *fakeRollups
	}{
		{name: "no raw events", repo: &fakeRollups{timezone: "UTC", hasTimezone: true}},
		{
			name: "range before the oldest event",
			repo: &fakeRollups{timezone: "UTC", hasTimezone: true, hasEvents: true, earliest: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := newTestAggregator(tt.repo, "UTC").Backfill(t.Context(),
				time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			assert.Equal(t, AggregateResult{}, result)
			assert.Empty(t, tt.repo.periods)
		})
	}
}

func TestAggregatorBackfillClipsToStoredEvents(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{
		timezone: "UTC", hasTimezone: true, hasEvents: true, earliest: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
	}

	result, err := newTestAggregator(repo, "UTC").Backfill(t.Context(),
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	assert.Equal(t, "day 2026-09-20", repo.periods[0], "days before the oldest raw event are left alone")
	assert.Contains(t, repo.periods, "day 2026-09-25")
	assert.NotContains(t, repo.periods, "day 2026-09-26", "future days are skipped")
	assert.Equal(t, "2026-09-20", repo.cohorts[0])
	assert.Equal(t, len(repo.periods), result.Periods)
}

func TestAggregatorBackfillReadsDatesInTheSiteZone(t *testing.T) {
	t.Parallel()

	repo := &fakeRollups{
		timezone: "America/New_York", hasTimezone: true, hasEvents: true, earliest: aggregateNow.AddDate(0, -1, 0),
	}

	// Midnight UTC on the 10th is still the 9th in New York; the date itself is what counts.
	_, err := newTestAggregator(repo, "America/New_York").Backfill(t.Context(),
		time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, "day 2026-09-10", repo.periods[0])
}

func TestAggregatorStopsOnAFailedPeriod(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	repo := &fakeRollups{timezone: "UTC", hasTimezone: true, failPeriod: boom}

	_, err := newTestAggregator(repo, "UTC").Run(t.Context())
	require.ErrorIs(t, err, boom)
	assert.Len(t, repo.periods, 1)
}

func TestAggregatorRejectsAnUnknownZone(t *testing.T) {
	t.Parallel()

	_, err := newTestAggregator(&fakeRollups{}, "Mars/Olympus").Run(t.Context())
	require.Error(t, err)
}

func TestAggregatorFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	withEvents := func(f *fakeRollups) *fakeRollups {
		f.hasEvents, f.earliest = true, aggregateNow.AddDate(0, 0, -2)

		return f
	}

	tests := []struct {
		name     string
		repo     *fakeRollups
		zone     ports.TimezoneSource
		backfill bool
	}{
		{name: "site zone unavailable", repo: &fakeRollups{}, zone: failingZone{err: boom}},
		{name: "stored zone unreadable", repo: &fakeRollups{timezoneErr: boom}, zone: staticZone("UTC")},
		{name: "oldest event unreadable on rebuild", repo: &fakeRollups{earliestErr: boom}, zone: staticZone("UTC")},
		{
			name: "old rollups cannot be deleted",
			repo: withEvents(&fakeRollups{timezone: "UTC", hasTimezone: true, deleteErr: boom}),
			zone: staticZone("Asia/Jakarta"),
		},
		{name: "rebuild cannot refresh first-seen", repo: withEvents(&fakeRollups{refreshErr: boom}), zone: staticZone("UTC")},
		{name: "cohort recompute fails", repo: &fakeRollups{timezone: "UTC", hasTimezone: true, cohortErr: boom}, zone: staticZone("UTC")},
		{
			name:     "backfill cannot read the oldest event",
			repo:     &fakeRollups{timezone: "UTC", hasTimezone: true, earliestErr: boom},
			zone:     staticZone("UTC"),
			backfill: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agg := NewAggregator(tt.repo, tt.zone, &fixedClock{now: aggregateNow})

			var err error
			if tt.backfill {
				_, err = agg.Backfill(t.Context(), aggregateNow.AddDate(0, 0, -7), aggregateNow)
			} else {
				_, err = agg.Run(t.Context())
			}

			require.ErrorIs(t, err, boom)
		})
	}
}
