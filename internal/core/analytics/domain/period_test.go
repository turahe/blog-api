package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeriodOfBucketsInTheLocalZone(t *testing.T) {
	t.Parallel()

	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	// 2026-09-24 20:00 UTC is Friday 2026-09-25 03:00 in Jakarta (UTC+7).
	at := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)

	day := PeriodOf(GrainDay, at, jakarta)
	assert.Equal(t, "2026-09-25", day.Day())
	assert.Equal(t, time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC), day.From)
	assert.Equal(t, time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC), day.To)

	week := PeriodOf(GrainWeek, at, jakarta)
	assert.Equal(t, "2026-09-21", week.Day(), "weeks start on Monday")
	assert.Equal(t, 7*24*time.Hour, week.To.Sub(week.From))

	month := PeriodOf(GrainMonth, at, jakarta)
	assert.Equal(t, "2026-09-01", month.Day())
	assert.Equal(t, "2026-10-01", month.Next().Day())

	sunday := PeriodOf(GrainWeek, time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), time.UTC)
	assert.Equal(t, "2026-09-21", sunday.Day(), "Sunday closes the ISO week")
}

func TestPeriodOfFollowsDaylightSaving(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	spring := PeriodOf(GrainDay, time.Date(2026, 3, 8, 12, 0, 0, 0, newYork), newYork)
	assert.Equal(t, 23*time.Hour, spring.To.Sub(spring.From), "the spring-forward day is 23 hours")
	assert.Equal(t, "2026-03-09", spring.Next().Day())
}

func TestPeriodsBetweenCoversOverlappingPeriods(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)

	days := PeriodsBetween(GrainDay, from, to, time.UTC)
	require.Len(t, days, 3)
	assert.Equal(t, "2026-08-31", days[0].Day())
	assert.Equal(t, "2026-09-02", days[2].Day())

	months := PeriodsBetween(GrainMonth, from, to, time.UTC)
	require.Len(t, months, 2)
	assert.Equal(t, "2026-08-01", months[0].Day())

	weeks := PeriodsBetween(GrainWeek, from, to, time.UTC)
	require.Len(t, weeks, 1, "Monday 31 August through Wednesday 2 September is one week")
}

func TestCohortReturnsCompleteOnlyAfterTheirDay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	cohort := CohortOf(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), time.UTC, now)

	assert.Equal(t, "2026-09-18", cohort.Day.Day())
	assert.Equal(t, "2026-09-19", cohort.Returns[0].Day.Day())
	assert.True(t, cohort.Returns[0].Complete)
	assert.Equal(t, "2026-09-25", cohort.Returns[1].Day.Day())
	assert.False(t, cohort.Returns[1].Complete, "day 7 is today and has not ended")
	assert.False(t, cohort.Returns[2].Complete)
}
