package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRetentionRepo struct {
	rawLeft    int64
	rawBefore  []time.Time
	rollupDays []string
	saltsFrom  string
}

func (f *fakeRetentionRepo) PruneSalts(_ context.Context, keepFrom string) (int64, error) {
	f.saltsFrom = keepFrom

	return 2, nil
}

func (f *fakeRetentionRepo) PruneRaw(_ context.Context, before time.Time, limit int) (int64, error) {
	f.rawBefore = append(f.rawBefore, before)
	n := min(f.rawLeft, int64(limit))
	f.rawLeft -= n

	return n, nil
}

func (f *fakeRetentionRepo) PruneDayRollups(_ context.Context, day string) (int64, error) {
	f.rollupDays = append(f.rollupDays, day)

	return 7, nil
}

type fixedRetention struct{ days, months int }

func (f fixedRetention) RawRetentionDays(context.Context) (int, error) {
	return f.days, nil
}

func (f fixedRetention) DayRollupRetentionMonths(context.Context) (int, error) {
	return f.months, nil
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
