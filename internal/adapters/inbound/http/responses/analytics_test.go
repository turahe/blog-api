package responses

import (
	"testing"
	"time"

	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

// dayWindow is a window of whole UTC days, one period per day.
func dayWindow(days ...string) analyticsdomain.Window {
	w := analyticsdomain.Window{Grain: analyticsdomain.GrainDay}
	for _, day := range days {
		start, err := time.Parse(time.DateOnly, day)
		if err != nil {
			panic(err)
		}

		w.Periods = append(w.Periods, analyticsdomain.PeriodOf(analyticsdomain.GrainDay, start, time.UTC))
	}

	return w
}

func twoDayHeader(compare bool) analyticsservice.Header {
	return analyticsservice.Header{
		Timezone:   "UTC",
		Window:     dayWindow("2026-08-01", "2026-08-02"),
		Comparison: dayWindow("2026-07-30", "2026-07-31"),
		Compare:    compare,
	}
}

const twoDayHeaderJSON = `"timezone": "UTC", "grain": "day",
	"window": {"from": "2026-08-01", "to": "2026-08-02", "periods": 2}`

func sampleTotals(periods int) analyticsdomain.Totals {
	return analyticsdomain.Totals{
		SiteRow: analyticsdomain.SiteRow{
			Views: 10, Visitors: 6, Sessions: 4, Bounces: 1, FocusSeconds: 30, FocusViews: 3,
			Searches: 2, ZeroResultSearches: 1, SearchesWithClick: 1, SearchClicks: 3,
			ConsentedVisitors: 5, NewVisitors: 2,
		},
		Periods: periods,
	}
}

const sampleTotalsRatesJSON = `"views": 10, "sessions": 4, "bounces": 1, "bounceRate": 0.25,
	"pagesPerSession": 2.5, "avgTimeSeconds": 10, "searches": 2, "zeroResultSearches": 1,
	"searchClicks": 3, "searchCtr": 0.5, "newVisitors": 2`

func TestAnalyticsHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header analyticsservice.Header
		want   string
	}{
		{
			name:   "without comparison",
			header: twoDayHeader(false),
			want:   `{` + twoDayHeaderJSON + `, "comparison": null}`,
		},
		{
			name:   "with comparison",
			header: twoDayHeader(true),
			want:   `{` + twoDayHeaderJSON + `, "comparison": {"from": "2026-07-30", "to": "2026-07-31"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsHeader(tt.header))
		})
	}
}

func TestAnalyticsTotals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		totals analyticsdomain.Totals
		grain  analyticsdomain.Grain
		want   string
	}{
		{
			name:   "one period adds distinct visitors",
			totals: sampleTotals(1),
			grain:  analyticsdomain.GrainDay,
			want: `{` + sampleTotalsRatesJSON + `,
				"visitorDays": 6, "visitors": 6, "consentedVisitorDays": 5, "consentedVisitors": 5}`,
		},
		{
			name:   "several periods only sum per-period uniques",
			totals: sampleTotals(3),
			grain:  analyticsdomain.GrainWeek,
			want:   `{` + sampleTotalsRatesJSON + `, "visitorWeeks": 6, "consentedVisitorWeeks": 5}`,
		},
		{
			name:   "rates without a denominator are null",
			totals: analyticsdomain.Totals{Periods: 2},
			grain:  analyticsdomain.GrainMonth,
			want: `{"views": 0, "sessions": 0, "bounces": 0, "bounceRate": null, "pagesPerSession": null,
				"avgTimeSeconds": null, "searches": 0, "zeroResultSearches": 0, "searchClicks": 0,
				"searchCtr": null, "newVisitors": 0, "visitorMonths": 0, "consentedVisitorMonths": 0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsTotals(tt.totals, tt.grain))
		})
	}
}

func TestAnalyticsTotalsOrNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		totals *analyticsdomain.Totals
		want   string
	}{
		{name: "no comparison", totals: nil, want: `null`},
		{
			name:   "comparison",
			totals: new(sampleTotals(2)),
			want:   `{` + sampleTotalsRatesJSON + `, "visitorDays": 6, "consentedVisitorDays": 5}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsTotalsOrNil(tt.totals, analyticsdomain.GrainDay))
		})
	}
}

func TestAnalyticsSeries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []analyticsdomain.SiteRow
		want string
	}{
		{name: "no rows", rows: nil, want: `[]`},
		{
			name: "one point per period",
			rows: []analyticsdomain.SiteRow{
				sampleTotals(1).SiteRow,
				{Period: "2026-08-02"},
			},
			want: `[
				{"period": "", "views": 10, "visitors": 6, "sessions": 4, "bounceRate": 0.25,
				 "avgTimeSeconds": 10, "searches": 2, "searchCtr": 0.5, "newVisitors": 2},
				{"period": "2026-08-02", "views": 0, "visitors": 0, "sessions": 0, "bounceRate": null,
				 "avgTimeSeconds": null, "searches": 0, "searchCtr": null, "newVisitors": 0}
			]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsSeries(tt.rows))
		})
	}
}

func TestAnalyticsOverview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		overview analyticsservice.Overview
		want     string
	}{
		{
			name:     "empty window",
			overview: analyticsservice.Overview{Header: twoDayHeader(false), Totals: analyticsdomain.Totals{Periods: 2}},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"totals": {"views": 0, "sessions": 0, "bounces": 0, "bounceRate": null, "pagesPerSession": null,
					"avgTimeSeconds": null, "searches": 0, "zeroResultSearches": 0, "searchClicks": 0,
					"searchCtr": null, "newVisitors": 0, "visitorDays": 0, "consentedVisitorDays": 0},
				"previous": null, "series": [], "referrers": [],
				"audience": {"country": [], "device": [], "browser": []}}`,
		},
		{
			name: "single period with sources and audience",
			overview: analyticsservice.Overview{
				Header:    analyticsservice.Header{Timezone: "UTC", Window: dayWindow("2026-08-01")},
				Totals:    analyticsdomain.Totals{SiteRow: analyticsdomain.SiteRow{Visitors: 3}, Periods: 1},
				Previous:  &analyticsdomain.Totals{Periods: 1},
				Series:    []analyticsdomain.SiteRow{{Period: "2026-08-01", Visitors: 3}},
				Referrers: []analyticsdomain.ReferrerRow{{Host: "news.example", Sessions: 4, Visitors: 2}},
				Dimensions: []analyticsdomain.DimensionRow{
					{Dimension: "country", Value: "ID", Views: 5, Visitors: 3},
					{Dimension: "device", Value: "mobile", Views: 2, Visitors: 1},
					{Dimension: "country", Value: "NL", Views: 1, Visitors: 1},
					{Dimension: "os", Value: "linux", Views: 1, Visitors: 1},
				},
			},
			want: `{"timezone": "UTC", "grain": "day",
				"window": {"from": "2026-08-01", "to": "2026-08-01", "periods": 1}, "comparison": null,
				"totals": {"views": 0, "sessions": 0, "bounces": 0, "bounceRate": null, "pagesPerSession": null,
					"avgTimeSeconds": null, "searches": 0, "zeroResultSearches": 0, "searchClicks": 0,
					"searchCtr": null, "newVisitors": 0, "visitorDays": 3, "visitors": 3,
					"consentedVisitorDays": 0, "consentedVisitors": 0},
				"previous": {"views": 0, "sessions": 0, "bounces": 0, "bounceRate": null, "pagesPerSession": null,
					"avgTimeSeconds": null, "searches": 0, "zeroResultSearches": 0, "searchClicks": 0,
					"searchCtr": null, "newVisitors": 0, "visitorDays": 0, "visitors": 0,
					"consentedVisitorDays": 0, "consentedVisitors": 0},
				"series": [{"period": "2026-08-01", "views": 0, "visitors": 3, "sessions": 0, "bounceRate": null,
					"avgTimeSeconds": null, "searches": 0, "searchCtr": null, "newVisitors": 0}],
				"referrers": [{"host": "news.example", "sessions": 4, "visitorDays": 2, "visitors": 2}],
				"audience": {
					"country": [
						{"value": "ID", "views": 5, "visitorDays": 3, "visitors": 3},
						{"value": "NL", "views": 1, "visitorDays": 1, "visitors": 1}
					],
					"device": [{"value": "mobile", "views": 2, "visitorDays": 1, "visitors": 1}],
					"browser": [],
					"os": [{"value": "linux", "views": 1, "visitorDays": 1, "visitors": 1}]
				}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsOverview(tt.overview))
		})
	}
}

func TestAnalyticsPages(t *testing.T) {
	t.Parallel()

	row := analyticsdomain.PageRow{
		Path: "/posts/a", Views: 9, Visitors: 4, Entries: 3, Exits: 2, FocusSeconds: 12, FocusViews: 4, PreviousViews: 5,
	}

	const baseRow = `"path": "/posts/a", "views": 9, "entries": 3, "exits": 2, "avgTimeSeconds": 3, "visitorDays": 4`

	tests := []struct {
		name   string
		report analyticsservice.PagesReport
		want   string
	}{
		{
			name:   "no previous views by default",
			report: analyticsservice.PagesReport{Header: twoDayHeader(false), Sort: "views", Pages: []analyticsdomain.PageRow{row}},
			want:   `{` + twoDayHeaderJSON + `, "comparison": null, "sort": "views", "pages": [{` + baseRow + `}]}`,
		},
		{
			name:   "comparison adds the change",
			report: analyticsservice.PagesReport{Header: twoDayHeader(true), Sort: "views", Pages: []analyticsdomain.PageRow{row}},
			want: `{` + twoDayHeaderJSON + `, "comparison": {"from": "2026-07-30", "to": "2026-07-31"},
				"sort": "views", "pages": [{` + baseRow + `, "previousViews": 5, "change": 4}]}`,
		},
		{
			name:   "rising sort adds the change",
			report: analyticsservice.PagesReport{Header: twoDayHeader(false), Sort: analyticsdomain.SortRising, Pages: []analyticsdomain.PageRow{row}},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"sort": "rising", "pages": [{` + baseRow + `, "previousViews": 5, "change": 4}]}`,
		},
		{
			name:   "no pages",
			report: analyticsservice.PagesReport{Header: twoDayHeader(false), Sort: "views"},
			want:   `{` + twoDayHeaderJSON + `, "comparison": null, "sort": "views", "pages": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsPages(tt.report))
		})
	}
}

func TestAnalyticsNavigation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report analyticsservice.NavigationReport
		want   string
	}{
		{
			name:   "empty",
			report: analyticsservice.NavigationReport{Header: twoDayHeader(false)},
			want:   `{` + twoDayHeaderJSON + `, "comparison": null, "transitions": [], "entries": [], "exits": []}`,
		},
		{
			name: "steps and pages",
			report: analyticsservice.NavigationReport{
				Header:      twoDayHeader(false),
				Transitions: []analyticsdomain.TransitionRow{{From: "/", To: "/posts/a", Transition: "link", Count: 7}},
				Entries:     []analyticsdomain.PathCount{{Path: "/", Count: 5}},
				Exits:       []analyticsdomain.PathCount{{Path: "/posts/a", Count: 3}, {Path: "/about", Count: 1}},
			},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"transitions": [{"from": "/", "to": "/posts/a", "transition": "link", "count": 7}],
				"entries": [{"path": "/", "count": 5}],
				"exits": [{"path": "/posts/a", "count": 3}, {"path": "/about", "count": 1}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsNavigation(tt.report))
		})
	}
}

func TestAnalyticsRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report analyticsservice.RetentionReport
		want   string
	}{
		{
			name:   "no cohorts",
			report: analyticsservice.RetentionReport{Header: twoDayHeader(false), Totals: analyticsdomain.Totals{Periods: 2}},
			want: `{` + twoDayHeaderJSON + `, "comparison": null, "cohorts": [],
				"rates": {"day1": null, "day7": null, "day30": null},
				"consented": {"newVisitors": 0, "consentedVisitorDays": 0}}`,
		},
		{
			name: "rates weight only cohorts whose day has ended",
			report: analyticsservice.RetentionReport{
				Header: twoDayHeader(false),
				Cohorts: []analyticsdomain.CohortRow{
					{Day: "2026-08-01", Size: 10, Day1: new(int64(4)), Day7: new(int64(2))},
					{Day: "2026-08-02", Size: 30, Day1: new(int64(6))},
				},
				Totals: analyticsdomain.Totals{SiteRow: analyticsdomain.SiteRow{NewVisitors: 7, ConsentedVisitors: 9}, Periods: 2},
			},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"cohorts": [
					{"day": "2026-08-01", "size": 10, "day1": 4, "day7": 2, "day30": null},
					{"day": "2026-08-02", "size": 30, "day1": 6, "day7": null, "day30": null}
				],
				"rates": {"day1": 0.25, "day7": 0.2, "day30": null},
				"consented": {"newVisitors": 7, "consentedVisitorDays": 9}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsRetention(tt.report))
		})
	}
}

func TestAnalyticsSearch(t *testing.T) {
	t.Parallel()

	const zeroTotals = `"views": 0, "sessions": 0, "bounces": 0, "bounceRate": null, "pagesPerSession": null,
		"avgTimeSeconds": null, "searches": 0, "zeroResultSearches": 0, "searchClicks": 0,
		"searchCtr": null, "newVisitors": 0, "visitorDays": 0, "consentedVisitorDays": 0`

	tests := []struct {
		name   string
		report analyticsservice.SearchReport
		want   string
	}{
		{
			name:   "no searches",
			report: analyticsservice.SearchReport{Header: twoDayHeader(false), Totals: analyticsdomain.Totals{Periods: 2}},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"totals": {` + zeroTotals + `, "avgSecondsToClick": null},
				"previous": null, "series": [], "queries": [], "zeroResultQueries": [],
				"positions": [], "clickedResults": []}`,
		},
		{
			name: "queries, positions, and clicked results",
			report: analyticsservice.SearchReport{
				Header:    twoDayHeader(true),
				Totals:    analyticsdomain.Totals{Periods: 2},
				Previous:  &analyticsdomain.Totals{Periods: 2},
				ClickTime: analyticsdomain.QueryRow{SearchesWithClick: 4, ClickSeconds: 10},
				Queries: []analyticsdomain.QueryRow{
					{Query: "go", Searches: 4, Visitors: 3, ZeroResults: 0, SearchesWithClick: 2, Clicks: 3, ClickSeconds: 5},
				},
				ZeroResults: []analyticsdomain.QueryRow{{Query: "rust", Searches: 2, Visitors: 2, ZeroResults: 2}},
				Positions:   []analyticsdomain.PositionRow{{Position: 1, Clicks: 3}, {Position: 2, Clicks: 1}},
				Results: []analyticsdomain.ResultRow{
					{Query: "go", ResourceType: "post", ResourceUUID: "0198a1b2-0000-7000-8000-000000000001", Clicks: 3},
				},
			},
			want: `{` + twoDayHeaderJSON + `, "comparison": {"from": "2026-07-30", "to": "2026-07-31"},
				"totals": {` + zeroTotals + `, "avgSecondsToClick": 2.5},
				"previous": {` + zeroTotals + `},
				"series": [],
				"queries": [{"query": "go", "searches": 4, "zeroResults": 0, "clicks": 3, "ctr": 0.5,
					"avgSecondsToClick": 2.5, "visitorDays": 3}],
				"zeroResultQueries": [{"query": "rust", "searches": 2, "zeroResults": 2, "clicks": 0, "ctr": 0,
					"avgSecondsToClick": null, "visitorDays": 2}],
				"positions": [{"position": 1, "clicks": 3, "share": 0.75}, {"position": 2, "clicks": 1, "share": 0.25}],
				"clickedResults": [{"query": "go", "resourceType": "post",
					"resourceId": "0198a1b2-0000-7000-8000-000000000001", "clicks": 3}]}`,
		},
		{
			name: "positions without clicks have no share",
			report: analyticsservice.SearchReport{
				Header:    twoDayHeader(false),
				Totals:    analyticsdomain.Totals{Periods: 2},
				Positions: []analyticsdomain.PositionRow{{Position: 1}},
			},
			want: `{` + twoDayHeaderJSON + `, "comparison": null,
				"totals": {` + zeroTotals + `, "avgSecondsToClick": null},
				"previous": null, "series": [], "queries": [], "zeroResultQueries": [],
				"positions": [{"position": 1, "clicks": 0, "share": null}], "clickedResults": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsSearch(tt.report))
		})
	}
}

func TestAnalyticsLiveSummary(t *testing.T) {
	t.Parallel()

	jakarta := time.FixedZone("WIB", 7*60*60)
	at := time.Date(2026, 8, 1, 17, 30, 15, 0, jakarta)
	minute := time.Date(2026, 8, 1, 10, 29, 0, 0, time.UTC)

	tests := []struct {
		name     string
		snapshot analyticsdomain.LiveSnapshot
		want     string
	}{
		{
			name:     "quiet",
			snapshot: analyticsdomain.LiveSnapshot{At: at},
			want: `{"ts": "2026-08-01T10:30:15Z", "activeSessions": 0, "activeSessionsCapped": false,
				"activeWindowMinutes": 5, "windowMinutes": 30, "views": 0, "searches": 0,
				"series": [], "topPages": []}`,
		},
		{
			name: "busy",
			snapshot: analyticsdomain.LiveSnapshot{
				At:             at,
				ActiveSessions: 100000,
				SessionsCapped: true,
				Series: []analyticsdomain.LiveMinute{
					{Start: minute, Views: 4, Searches: 1},
					{Start: minute.Add(time.Minute), Views: 2, Searches: 2},
				},
				TopPages: []analyticsdomain.PathCount{{Path: "/", Count: 6}},
			},
			want: `{"ts": "2026-08-01T10:30:15Z", "activeSessions": 100000, "activeSessionsCapped": true,
				"activeWindowMinutes": 5, "windowMinutes": 30, "views": 6, "searches": 3,
				"series": [
					{"minute": "2026-08-01T10:29:00Z", "views": 4, "searches": 1},
					{"minute": "2026-08-01T10:30:00Z", "views": 2, "searches": 2}
				],
				"topPages": [{"path": "/", "views": 6}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AnalyticsLiveSummary(tt.snapshot))
		})
	}
}

func TestAnalyticsLivePageView(t *testing.T) {
	t.Parallel()

	event := analyticsdomain.LiveEvent{
		At:      time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		Path:    "/posts/a",
		Country: "ID",
		Device:  analyticsdomain.DeviceMobile,
		Query:   "ignored",
	}

	assertJSON(t, `{"ts": "2026-08-01T10:00:00Z", "path": "/posts/a", "country": "ID", "device": "mobile"}`,
		AnalyticsLivePageView(event))
}

func TestAnalyticsLiveSearch(t *testing.T) {
	t.Parallel()

	event := analyticsdomain.LiveEvent{
		At:      time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		Path:    "ignored",
		Query:   "go generics",
		Results: 0,
	}

	assertJSON(t, `{"ts": "2026-08-01T10:00:00Z", "query": "go generics", "resultCount": 0}`, AnalyticsLiveSearch(event))
}
