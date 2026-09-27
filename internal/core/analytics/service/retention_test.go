package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

type fakeRetentionRepo struct {
	rawLeft    int64
	rawBefore  []time.Time
	rollupDays []string
	saltsFrom  string
	saltsErr   error
	rawErr     error
}

func (f *fakeRetentionRepo) PruneSalts(_ context.Context, keepFrom string) (int64, error) {
	if f.saltsErr != nil {
		return 0, f.saltsErr
	}

	f.saltsFrom = keepFrom

	return 2, nil
}

func (f *fakeRetentionRepo) PruneRaw(_ context.Context, before time.Time, limit int) (int64, error) {
	if f.rawErr != nil {
		return 0, f.rawErr
	}

	f.rawBefore = append(f.rawBefore, before)
	n := min(f.rawLeft, int64(limit))
	f.rawLeft -= n

	return n, nil
}

func (f *fakeRetentionRepo) PruneDayRollups(_ context.Context, day string) (int64, error) {
	f.rollupDays = append(f.rollupDays, day)

	return 7, nil
}

type fixedRetention struct {
	days, months       int
	daysErr, monthsErr error
}

func (f fixedRetention) RawRetentionDays(context.Context) (int, error) {
	return f.days, f.daysErr
}

func (f fixedRetention) DayRollupRetentionMonths(context.Context) (int, error) {
	return f.months, f.monthsErr
}

func TestRetentionRunPrunesInBatches(t *testing.T) {
	t.Parallel()

	repo := &fakeRetentionRepo{rawLeft: 2*retentionBatch + 10}
	clock := &fixedClock{now: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)}

	result, err := NewRetention(repo, fixedRetention{days: 90, months: 25}, fixedTimezone("UTC"), clock).Run(t.Context())
	require.NoError(t, err)

	want := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, want, result.RawBefore)
	assert.Equal(t, int64(2*retentionBatch+10), result.RawDeleted)
	assert.Len(t, repo.rawBefore, 4, "three deleting batches and one empty one")
	assert.Equal(t, []string{"2024-08-25"}, repo.rollupDays)
	assert.Equal(t, "2024-08-25", result.RollupsBefore)
	assert.Equal(t, int64(7), result.RollupsDeleted)
	assert.Equal(t, "2026-09-24", repo.saltsFrom, "only today's and yesterday's salts survive")
	assert.Equal(t, int64(2), result.SaltsDeleted)
}

func TestRetentionRunKeepsDailyRollupsForeverAtZero(t *testing.T) {
	t.Parallel()

	repo := &fakeRetentionRepo{}
	clock := &fixedClock{now: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)}

	result, err := NewRetention(repo, fixedRetention{days: 1, months: 0}, fixedTimezone("UTC"), clock).Run(t.Context())
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), result.RawBefore, "floor: the previous month")
	assert.Empty(t, repo.rollupDays)
	assert.Empty(t, result.RollupsBefore)
}

func TestRetentionRunFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	settings := fixedRetention{days: 90, months: 25}

	tests := []struct {
		name     string
		repo     *fakeRetentionRepo
		settings fixedRetention
		tz       ports.TimezoneSource
		wantErr  error
	}{
		{name: "time zone unavailable", repo: &fakeRetentionRepo{}, settings: settings, tz: failingZone{err: boom}, wantErr: boom},
		{name: "raw retention unavailable", repo: &fakeRetentionRepo{}, settings: fixedRetention{daysErr: boom}, tz: fixedTimezone("UTC"), wantErr: boom},
		{name: "rollup retention unavailable", repo: &fakeRetentionRepo{}, settings: fixedRetention{days: 90, monthsErr: boom}, tz: fixedTimezone("UTC"), wantErr: boom},
		{name: "salt prune fails", repo: &fakeRetentionRepo{saltsErr: boom}, settings: settings, tz: fixedTimezone("UTC"), wantErr: boom},
		{name: "raw prune fails", repo: &fakeRetentionRepo{rawErr: boom}, settings: settings, tz: fixedTimezone("UTC"), wantErr: boom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clock := &fixedClock{now: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)}

			_, err := NewRetention(tt.repo, tt.settings, tt.tz, clock).Run(t.Context())
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRetentionRunRejectsAnUnknownTimeZone(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)}
	repo := &fakeRetentionRepo{}

	_, err := NewRetention(repo, fixedRetention{days: 90}, fixedTimezone("Mars/Olympus"), clock).Run(t.Context())
	require.ErrorContains(t, err, `site time zone "Mars/Olympus"`)
	assert.Empty(t, repo.saltsFrom, "nothing is pruned")
}
