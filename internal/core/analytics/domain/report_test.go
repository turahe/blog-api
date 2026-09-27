package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidGrain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		grain Grain
		want  bool
	}{
		{name: "day", grain: GrainDay, want: true},
		{name: "week", grain: GrainWeek, want: true},
		{name: "month", grain: GrainMonth, want: true},
		{name: "empty", grain: "", want: false},
		{name: "year", grain: "year", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ValidGrain(tt.grain))
		})
	}
}

func TestAutoGrain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		days int
		want Grain
	}{
		{name: "a week", days: 7, want: GrainDay},
		{name: "the daily cap", days: MaxDailyReportDays, want: GrainDay},
		{name: "just past the daily cap", days: MaxDailyReportDays + 1, want: GrainWeek},
		{name: "a leap year", days: 366, want: GrainWeek},
		{name: "over a year", days: 367, want: GrainMonth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, AutoGrain(tt.days))
		})
	}
}

func TestSiteRowAdd(t *testing.T) {
	t.Parallel()

	row := SiteRow{Period: "2026-09-01", Views: 1, Visitors: 1, NewVisitors: 1}
	row.Add(SiteRow{
		Period: "2026-09-02", Views: 2, Visitors: 3, Sessions: 4, Bounces: 5, FocusSeconds: 6, FocusViews: 7,
		Searches: 8, ZeroResultSearches: 9, SearchesWithClick: 10, SearchClicks: 11, ConsentedVisitors: 12, NewVisitors: 13,
	})

	assert.Equal(t, SiteRow{
		Period: "2026-09-01", Views: 3, Visitors: 4, Sessions: 4, Bounces: 5, FocusSeconds: 6, FocusViews: 7,
		Searches: 8, ZeroResultSearches: 9, SearchesWithClick: 10, SearchClicks: 11, ConsentedVisitors: 12, NewVisitors: 14,
	}, row, "the period label is kept")
}
