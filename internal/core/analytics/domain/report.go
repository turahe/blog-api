package domain

import "time"

// Admin analytics permissions.
const (
	PermRead       = "analytics.read"
	PermSearchRead = "analytics.search.read"
)

// Report limits.
const (
	DefaultReportDays = 30
	MaxReportDays     = 731
	// MaxDailyReportDays is the longest dashboard range at day grain; longer ranges read
	// weekly or monthly rollups so a report scans a bounded number of rows.
	MaxDailyReportDays = 92
	DefaultReportLimit = 20
	MaxReportLimit     = 100
)

// Page report orders.
const (
	SortViews  = "views"
	SortTime   = "time"
	SortRising = "rising"
)

// ReportQuery selects a dashboard window. Zero From and To default to the DefaultReportDays
// days through today; From and To are calendar dates read in the site time zone. An empty
// Grain is picked from the range length.
type ReportQuery struct {
	From, To time.Time
	Grain    Grain
	Compare  bool
	Limit    int
	Sort     string
}

// ValidGrain reports whether g is a rollup grain.
func ValidGrain(g Grain) bool {
	return g == GrainDay || g == GrainWeek || g == GrainMonth
}

// AutoGrain picks the grain for a range of days: daily up to about a quarter, weekly up to a
// year, monthly beyond.
func AutoGrain(days int) Grain {
	switch {
	case days <= MaxDailyReportDays:
		return GrainDay
	case days <= 366:
		return GrainWeek
	default:
		return GrainMonth
	}
}

// Window is a run of consecutive periods of one grain. Reports cover whole periods, so a
// weekly or monthly window can start before and end after the requested dates.
type Window struct {
	Grain   Grain
	Periods []Period
}

// FirstDay is the window's first local date.
func (w Window) FirstDay() string {
	return w.Periods[0].Day()
}

// LastDay is the window's last local date.
func (w Window) LastDay() string {
	last := w.Periods[len(w.Periods)-1]

	return last.To.In(last.Start.Location()).AddDate(0, 0, -1).Format(time.DateOnly)
}

// Selection is the rollup rows of the window: its grain and first and last period_start.
func (w Window) Selection() Selection {
	return Selection{Grain: w.Grain, First: w.FirstDay(), Last: w.Periods[len(w.Periods)-1].Day()}
}

// Selection picks rollup rows by grain and period_start range, both ends inclusive.
type Selection struct {
	Grain       Grain
	First, Last string
}

// SiteRow is one period of analytics_rollup_site; as a total it sums every period, so
// Visitors and ConsentedVisitors are then per-period uniques added up.
type SiteRow struct {
	Period                                                        string
	Views, Visitors, Sessions, Bounces, FocusSeconds, FocusViews  int64
	Searches, ZeroResultSearches, SearchesWithClick, SearchClicks int64
	ConsentedVisitors, NewVisitors                                int64
}

// Add accumulates another period into r.
func (r *SiteRow) Add(o SiteRow) {
	r.Views += o.Views
	r.Visitors += o.Visitors
	r.Sessions += o.Sessions
	r.Bounces += o.Bounces
	r.FocusSeconds += o.FocusSeconds
	r.FocusViews += o.FocusViews
	r.Searches += o.Searches
	r.ZeroResultSearches += o.ZeroResultSearches
	r.SearchesWithClick += o.SearchesWithClick
	r.SearchClicks += o.SearchClicks
	r.ConsentedVisitors += o.ConsentedVisitors
	r.NewVisitors += o.NewVisitors
}

// Totals sums a window's site rows. Periods is how many periods were summed: visitor counts are
// distinct people only when it is 1.
type Totals struct {
	SiteRow

	Periods int
}

// ReferrerRow is a traffic source summed over a window.
type ReferrerRow struct {
	Host               string
	Sessions, Visitors int64
}

// DimensionRow is a country, device, or browser value summed over a window.
type DimensionRow struct {
	Dimension, Value string
	Views, Visitors  int64
}

// PageRow is a path summed over a window, with its views in the previous window.
type PageRow struct {
	Path                                    string
	Views, Visitors, Entries, Exits         int64
	FocusSeconds, FocusViews, PreviousViews int64
}

// TransitionRow is a navigation step summed over a window.
type TransitionRow struct {
	From, To, Transition string
	Count                int64
}

// PathCount is a path with a count, for entry and exit lists.
type PathCount struct {
	Path  string
	Count int64
}

// SearchQueries are the query lists of a search report.
type SearchQueries struct {
	// Totals sums every query row, including the folded remainder.
	Totals QueryRow
	// Top orders by searches; ZeroResults by zero-result searches, leaving out the remainder.
	Top, ZeroResults []QueryRow
}

// QueryRow is a search query summed over a window.
type QueryRow struct {
	Query                                                      string
	Searches, Visitors, ZeroResults, SearchesWithClick, Clicks int64
	ClickSeconds                                               int64
}

// PositionRow is the clicks on one result position.
type PositionRow struct {
	Position int
	Clicks   int64
}

// ResultRow is a clicked result of a query.
type ResultRow struct {
	Query, ResourceType, ResourceUUID string
	Clicks                            int64
}

// CohortRow is one retention cohort; a return is nil until its day has ended.
type CohortRow struct {
	Day               string
	Size              int64
	Day1, Day7, Day30 *int64
}
