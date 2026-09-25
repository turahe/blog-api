package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawRetentionCutoffKeepsWhatTheAggregatorRecomputes(t *testing.T) {
	t.Parallel()

	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	cases := map[string]struct {
		now  time.Time
		days int
		want string
	}{
		"setting wins when older":         {time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC), 90, "2026-06-27"},
		"previous month floor":            {time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC), 1, "2026-08-01"},
		"31 day floor on the first":       {time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), 1, "2026-08-01"},
		"previous month beats 31 days":    {time.Date(2026, 3, 31, 3, 0, 0, 0, time.UTC), 31, "2026-02-01"},
		"local date, not UTC date":        {time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC), 1, "2026-08-31"},
		"january reaches back a year end": {time.Date(2026, 1, 15, 3, 0, 0, 0, time.UTC), 1, "2025-12-01"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := RawRetentionCutoff(tc.now, jakarta, tc.days)
			assert.Equal(t, tc.want, got.Format(time.DateOnly))
			assert.Equal(t, jakarta, got.Location())
			assert.Zero(t, got.Hour())
		})
	}
}

func TestDayRollupCutoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	day, ok := DayRollupCutoff(now, time.UTC, 25)
	require.True(t, ok)
	assert.Equal(t, "2024-08-25", day)

	_, ok = DayRollupCutoff(now, time.UTC, 0)
	assert.False(t, ok, "0 keeps daily rollups forever")
}

func TestExportStateAndWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)
	key := "analytics-exports/x.zip"

	assert.Equal(t, "pending", Export{Status: ExportPending}.State(now))
	assert.True(t, Export{Status: ExportRunning}.Open())
	assert.Equal(t, "completed", Export{Status: ExportCompleted, StorageKey: &key, ExpiresAt: &later}.State(now))
	assert.Equal(t, ExportExpired, Export{Status: ExportCompleted, StorageKey: &key, ExpiresAt: &earlier}.State(now))
	assert.Equal(t, ExportExpired, Export{Status: ExportCompleted, ExpiresAt: &later}.State(now), "archive already deleted")

	window, err := Export{Grain: GrainMonth, FirstDay: "2026-08-01", LastDay: "2026-09-30", Timezone: "Asia/Jakarta"}.Window()
	require.NoError(t, err)
	assert.Equal(t, Selection{Grain: GrainMonth, First: "2026-08-01", Last: "2026-09-01"}, window.Selection())
	assert.Equal(t, "2026-09-30", window.LastDay())

	_, err = Export{Grain: GrainDay, FirstDay: "2026-08-01", LastDay: "2026-08-02", Timezone: "Nowhere/City"}.Window()
	require.Error(t, err)
}
