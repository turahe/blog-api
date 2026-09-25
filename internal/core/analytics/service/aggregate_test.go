package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
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
}

func (f *fakeRollups) EarliestEvent(context.Context) (time.Time, bool, error) {
	return f.earliest, f.hasEvents, nil
}

func (f *fakeRollups) Timezone(context.Context) (string, bool, error) {
	return f.timezone, f.hasTimezone, nil
}

func (f *fakeRollups) SetTimezone(_ context.Context, timezone string, _ time.Time) error {
	f.timezone, f.hasTimezone = timezone, true

	return nil
}

func (f *fakeRollups) DeleteFrom(_ context.Context, day string) error {
	f.deletedFrom = append(f.deletedFrom, day)

	return nil
}

func (f *fakeRollups) RefreshFirstSeen(_ context.Context, from, to time.Time) error {
	f.firstSeen = append(f.firstSeen, [2]time.Time{from, to})

	return nil
}

func (f *fakeRollups) RecomputePeriod(_ context.Context, period domain.Period, _ int, _ time.Time) error {
	f.periods = append(f.periods, string(period.Grain)+" "+period.Day())

	return f.failPeriod
}

func (f *fakeRollups) RecomputeCohort(_ context.Context, cohort domain.Cohort, _ time.Time) error {
	f.cohorts = append(f.cohorts, cohort.Day.Day())

	return nil
}

type staticZone string

func (z staticZone) Timezone(context.Context) (string, error) { return string(z), nil }

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
