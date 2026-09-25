package domain

import "time"

// Grain is the length of a rollup period.
type Grain string

// Rollup grains. Weeks are ISO weeks starting on Monday.
const (
	GrainDay   Grain = "day"
	GrainWeek  Grain = "week"
	GrainMonth Grain = "month"
)

// Grains lists every rollup grain.
var Grains = []Grain{GrainDay, GrainWeek, GrainMonth}

// RollupTopN is how many paths, referrer hosts, transitions, queries, and clicked results a
// period keeps; the rest are folded into Other (clicked results are simply cut).
const RollupTopN = 1000

// Other is the folded remainder of a capped dimension. Real paths start with "/" and cannot
// collide with it.
const Other = "(other)"

// CohortDays are the retention offsets a cohort reports.
var CohortDays = [3]int{1, 7, 30}

// Period is one rollup bucket: Start is its first local midnight, and [From, To) its bounds
// in UTC.
type Period struct {
	Grain Grain
	Start time.Time
	From  time.Time
	To    time.Time
}

// PeriodOf returns the period of grain containing t in loc.
func PeriodOf(grain Grain, t time.Time, loc *time.Location) Period {
	year, month, day := t.In(loc).Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, loc)

	end := start.AddDate(0, 0, 1)

	switch grain {
	case GrainWeek:
		start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
		end = start.AddDate(0, 0, 7)
	case GrainMonth:
		start = time.Date(year, month, 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
	case GrainDay:
	}

	return Period{Grain: grain, Start: start, From: start.UTC(), To: end.UTC()}
}

// Day returns the local calendar date of the period's start, as stored in period_start.
func (p Period) Day() string {
	return p.Start.Format(time.DateOnly)
}

// Next returns the following period of the same grain.
func (p Period) Next() Period {
	return PeriodOf(p.Grain, p.To, p.Start.Location())
}

// PeriodsBetween returns the periods of grain overlapping the local days from through to.
func PeriodsBetween(grain Grain, from, to time.Time, loc *time.Location) []Period {
	end := PeriodOf(GrainDay, to, loc).To

	var periods []Period
	for p := PeriodOf(grain, from, loc); p.From.Before(end); p = p.Next() {
		periods = append(periods, p)
	}

	return periods
}

// Cohort is the retention window of the consented visitors first seen on one local day.
type Cohort struct {
	Day Period
	// Returns are the days CohortDays after Day; Complete is false until the day has ended.
	Returns [len(CohortDays)]CohortReturn
}

// CohortReturn is one retention day of a cohort.
type CohortReturn struct {
	Day      Period
	Complete bool
}

// CohortOf returns the cohort of the local day containing day, as of now.
func CohortOf(day time.Time, loc *time.Location, now time.Time) Cohort {
	cohort := Cohort{Day: PeriodOf(GrainDay, day, loc)}

	for i, offset := range CohortDays {
		ret := PeriodOf(GrainDay, cohort.Day.Start.AddDate(0, 0, offset), loc)
		cohort.Returns[i] = CohortReturn{Day: ret, Complete: !ret.To.After(now)}
	}

	return cohort
}
