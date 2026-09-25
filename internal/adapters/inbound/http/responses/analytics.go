package responses

import (
	"strconv"

	"github.com/gin-gonic/gin"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

// AnalyticsHeader renders the time zone, grain, and windows of a report. comparison is null
// unless the query asked for one.
func AnalyticsHeader(h analyticsservice.Header) gin.H {
	out := gin.H{
		"timezone":   h.Timezone,
		"grain":      h.Window.Grain,
		"window":     gin.H{"from": h.Window.FirstDay(), "to": h.Window.LastDay(), "periods": len(h.Window.Periods)},
		"comparison": nil,
	}

	if h.Compare {
		out["comparison"] = gin.H{"from": h.Comparison.FirstDay(), "to": h.Comparison.LastDay()}
	}

	return out
}

// visitorKey names a count of per-period uniques added up: visitor_days, visitor_weeks, or
// visitor_months.
func visitorKey(prefix string, grain analyticsdomain.Grain) string {
	return prefix + "_" + string(grain) + "s"
}

// setVisitors adds the summed per-period uniques under their grain key and, when the window is
// one period, the distinct count under plain key.
func setVisitors(out gin.H, key string, grain analyticsdomain.Grain, periods int, sum int64) {
	out[visitorKey(key, grain)] = sum
	if periods == 1 {
		out[key+"s"] = sum
	}
}

// AnalyticsTotals renders summed site measures with their rates; a rate without a
// denominator is null.
func AnalyticsTotals(t analyticsdomain.Totals, grain analyticsdomain.Grain) gin.H {
	out := gin.H{
		"views":                t.Views,
		"sessions":             t.Sessions,
		"bounces":              t.Bounces,
		"bounce_rate":          ratio(t.Bounces, t.Sessions),
		"pages_per_session":    ratio(t.Views, t.Sessions),
		"avg_time_seconds":     ratio(t.FocusSeconds, t.FocusViews),
		"searches":             t.Searches,
		"zero_result_searches": t.ZeroResultSearches,
		"search_clicks":        t.SearchClicks,
		"search_ctr":           ratio(t.SearchesWithClick, t.Searches),
		"new_visitors":         t.NewVisitors,
	}
	setVisitors(out, "visitor", grain, t.Periods, t.Visitors)
	setVisitors(out, "consented_visitor", grain, t.Periods, t.ConsentedVisitors)

	return out
}

// AnalyticsTotalsOrNil renders previous-window totals, or null without a comparison.
func AnalyticsTotalsOrNil(t *analyticsdomain.Totals, grain analyticsdomain.Grain) any {
	if t == nil {
		return nil
	}

	return AnalyticsTotals(*t, grain)
}

// AnalyticsSeries renders one point per period; visitors are distinct within each period.
func AnalyticsSeries(rows []analyticsdomain.SiteRow) []gin.H {
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{
			"period": r.Period, "views": r.Views, "visitors": r.Visitors, "sessions": r.Sessions,
			"bounce_rate": ratio(r.Bounces, r.Sessions), "avg_time_seconds": ratio(r.FocusSeconds, r.FocusViews),
			"searches": r.Searches, "search_ctr": ratio(r.SearchesWithClick, r.Searches),
			"new_visitors": r.NewVisitors,
		})
	}

	return out
}

// AnalyticsOverview renders the dashboard summary.
func AnalyticsOverview(o analyticsservice.Overview) gin.H {
	grain := o.Window.Grain
	out := AnalyticsHeader(o.Header)
	out["totals"] = AnalyticsTotals(o.Totals, grain)
	out["previous"] = AnalyticsTotalsOrNil(o.Previous, grain)
	out["series"] = AnalyticsSeries(o.Series)

	referrers := make([]gin.H, 0, len(o.Referrers))
	for _, r := range o.Referrers {
		item := gin.H{"host": r.Host, "sessions": r.Sessions}
		setVisitors(item, "visitor", grain, o.Totals.Periods, r.Visitors)
		referrers = append(referrers, item)
	}

	dimensions := gin.H{"country": []gin.H{}, "device": []gin.H{}, "browser": []gin.H{}}

	for _, d := range o.Dimensions {
		item := gin.H{"value": d.Value, "views": d.Views}
		setVisitors(item, "visitor", grain, o.Totals.Periods, d.Visitors)

		list, _ := dimensions[d.Dimension].([]gin.H)
		dimensions[d.Dimension] = append(list, item)
	}

	out["referrers"] = referrers
	out["audience"] = dimensions

	return out
}

// AnalyticsPages renders the page list. previous_views and change appear when comparing or
// sorting by rising.
func AnalyticsPages(p analyticsservice.PagesReport) gin.H {
	grain, periods := p.Window.Grain, len(p.Window.Periods)
	withPrevious := p.Compare || p.Sort == analyticsdomain.SortRising

	pages := make([]gin.H, 0, len(p.Pages))
	for _, row := range p.Pages {
		item := gin.H{
			"path": row.Path, "views": row.Views, "entries": row.Entries, "exits": row.Exits,
			"avg_time_seconds": ratio(row.FocusSeconds, row.FocusViews),
		}
		setVisitors(item, "visitor", grain, periods, row.Visitors)

		if withPrevious {
			item["previous_views"], item["change"] = row.PreviousViews, row.Views-row.PreviousViews
		}

		pages = append(pages, item)
	}

	out := AnalyticsHeader(p.Header)
	out["sort"], out["pages"] = p.Sort, pages

	return out
}

// AnalyticsNavigation renders navigation steps and entry and exit pages.
func AnalyticsNavigation(n analyticsservice.NavigationReport) gin.H {
	transitions := make([]gin.H, 0, len(n.Transitions))
	for _, t := range n.Transitions {
		transitions = append(transitions, gin.H{"from": t.From, "to": t.To, "transition": t.Transition, "count": t.Count})
	}

	out := AnalyticsHeader(n.Header)
	out["transitions"], out["entries"], out["exits"] = transitions, pathCounts(n.Entries), pathCounts(n.Exits)

	return out
}

// AnalyticsRetention renders cohorts with their weighted return rates, and the new versus
// returning split of consented visitors.
func AnalyticsRetention(r analyticsservice.RetentionReport) gin.H {
	cohorts := make([]gin.H, 0, len(r.Cohorts))

	var size, returned [len(analyticsdomain.CohortDays)]int64

	for _, c := range r.Cohorts {
		item := gin.H{"day": c.Day, "size": c.Size}

		for i, ret := range []*int64{c.Day1, c.Day7, c.Day30} {
			key := "day" + strconv.Itoa(analyticsdomain.CohortDays[i])
			item[key] = ret

			if ret != nil {
				size[i] += c.Size
				returned[i] += *ret
			}
		}

		cohorts = append(cohorts, item)
	}

	rates := gin.H{}
	for i, day := range analyticsdomain.CohortDays {
		rates["day"+strconv.Itoa(day)] = ratio(returned[i], size[i])
	}

	grain := r.Window.Grain
	visitors := gin.H{"new_visitors": r.Totals.NewVisitors}
	setVisitors(visitors, "consented_visitor", grain, r.Totals.Periods, r.Totals.ConsentedVisitors)

	out := AnalyticsHeader(r.Header)
	out["cohorts"], out["rates"], out["consented"] = cohorts, rates, visitors

	return out
}

// AnalyticsSearch renders search totals, series, queries, positions, and clicked results.
func AnalyticsSearch(s analyticsservice.SearchReport) gin.H {
	grain, periods := s.Window.Grain, len(s.Window.Periods)

	var clicks int64
	for _, p := range s.Positions {
		clicks += p.Clicks
	}

	positions := make([]gin.H, 0, len(s.Positions))
	for _, p := range s.Positions {
		positions = append(positions, gin.H{"position": p.Position, "clicks": p.Clicks, "share": ratio(p.Clicks, clicks)})
	}

	results := make([]gin.H, 0, len(s.Results))
	for _, r := range s.Results {
		results = append(results, gin.H{
			"query": r.Query, "resource_type": r.ResourceType, "resource_id": r.ResourceUUID, "clicks": r.Clicks,
		})
	}

	totals := AnalyticsTotals(s.Totals, grain)
	totals["avg_seconds_to_click"] = ratio(s.ClickTime.ClickSeconds, s.ClickTime.SearchesWithClick)

	out := AnalyticsHeader(s.Header)
	out["totals"], out["previous"] = totals, AnalyticsTotalsOrNil(s.Previous, grain)
	out["series"] = AnalyticsSeries(s.Series)
	out["queries"] = searchQueries(s.Queries, grain, periods)
	out["zero_result_queries"] = searchQueries(s.ZeroResults, grain, periods)
	out["positions"], out["clicked_results"] = positions, results

	return out
}

func searchQueries(rows []analyticsdomain.QueryRow, grain analyticsdomain.Grain, periods int) []gin.H {
	out := make([]gin.H, 0, len(rows))
	for _, q := range rows {
		item := gin.H{
			"query": q.Query, "searches": q.Searches, "zero_results": q.ZeroResults, "clicks": q.Clicks,
			"ctr": ratio(q.SearchesWithClick, q.Searches), "avg_seconds_to_click": ratio(q.ClickSeconds, q.SearchesWithClick),
		}
		setVisitors(item, "visitor", grain, periods, q.Visitors)
		out = append(out, item)
	}

	return out
}

func pathCounts(rows []analyticsdomain.PathCount) []gin.H {
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{"path": r.Path, "count": r.Count})
	}

	return out
}

// ratio returns part/whole, or nil when whole is zero.
func ratio(part, whole int64) any {
	if whole == 0 {
		return nil
	}

	return float64(part) / float64(whole)
}
