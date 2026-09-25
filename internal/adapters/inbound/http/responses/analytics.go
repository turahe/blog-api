package responses

import (
	"strconv"
	"time"

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

// visitorKey names a count of per-period uniques added up: visitorDays, visitorWeeks, or
// visitorMonths.
func visitorKey(prefix string, grain analyticsdomain.Grain) string {
	return CamelKey(prefix + "_" + string(grain) + "s")
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
		"views":              t.Views,
		"sessions":           t.Sessions,
		"bounces":            t.Bounces,
		"bounceRate":         ratio(t.Bounces, t.Sessions),
		"pagesPerSession":    ratio(t.Views, t.Sessions),
		"avgTimeSeconds":     ratio(t.FocusSeconds, t.FocusViews),
		"searches":           t.Searches,
		"zeroResultSearches": t.ZeroResultSearches,
		"searchClicks":       t.SearchClicks,
		"searchCtr":          ratio(t.SearchesWithClick, t.Searches),
		"newVisitors":        t.NewVisitors,
	}
	setVisitors(out, "visitor", grain, t.Periods, t.Visitors)
	setVisitors(out, "consentedVisitor", grain, t.Periods, t.ConsentedVisitors)

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
			"bounceRate": ratio(r.Bounces, r.Sessions), "avgTimeSeconds": ratio(r.FocusSeconds, r.FocusViews),
			"searches": r.Searches, "searchCtr": ratio(r.SearchesWithClick, r.Searches),
			"newVisitors": r.NewVisitors,
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

// AnalyticsPages renders the page list. previousViews and change appear when comparing or
// sorting by rising.
func AnalyticsPages(p analyticsservice.PagesReport) gin.H {
	grain, periods := p.Window.Grain, len(p.Window.Periods)
	withPrevious := p.Compare || p.Sort == analyticsdomain.SortRising

	pages := make([]gin.H, 0, len(p.Pages))
	for _, row := range p.Pages {
		item := gin.H{
			"path": row.Path, "views": row.Views, "entries": row.Entries, "exits": row.Exits,
			"avgTimeSeconds": ratio(row.FocusSeconds, row.FocusViews),
		}
		setVisitors(item, "visitor", grain, periods, row.Visitors)

		if withPrevious {
			item["previousViews"], item["change"] = row.PreviousViews, row.Views-row.PreviousViews
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
	visitors := gin.H{"newVisitors": r.Totals.NewVisitors}
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
			"query": r.Query, "resourceType": r.ResourceType, "resourceId": r.ResourceUUID, "clicks": r.Clicks,
		})
	}

	totals := AnalyticsTotals(s.Totals, grain)
	totals["avgSecondsToClick"] = ratio(s.ClickTime.ClickSeconds, s.ClickTime.SearchesWithClick)

	out := AnalyticsHeader(s.Header)
	out["totals"], out["previous"] = totals, AnalyticsTotalsOrNil(s.Previous, grain)
	out["series"] = AnalyticsSeries(s.Series)
	out["queries"] = searchQueries(s.Queries, grain, periods)
	out["zeroResultQueries"] = searchQueries(s.ZeroResults, grain, periods)
	out["positions"], out["clickedResults"] = positions, results

	return out
}

// AnalyticsLiveSummary renders the live counters.
func AnalyticsLiveSummary(s analyticsdomain.LiveSnapshot) gin.H {
	series := make([]gin.H, 0, len(s.Series))

	var views, searches int64

	for _, m := range s.Series {
		views += m.Views
		searches += m.Searches
		series = append(series, gin.H{"minute": m.Start.Format(time.RFC3339), "views": m.Views, "searches": m.Searches})
	}

	top := make([]gin.H, 0, len(s.TopPages))
	for _, p := range s.TopPages {
		top = append(top, gin.H{"path": p.Path, "views": p.Count})
	}

	return gin.H{
		"ts":                   s.At.UTC().Format(time.RFC3339),
		"activeSessions":       s.ActiveSessions,
		"activeSessionsCapped": s.SessionsCapped,
		"activeWindowMinutes":  int(analyticsdomain.LiveActiveWindow / time.Minute),
		"windowMinutes":        int(analyticsdomain.LiveWindow / time.Minute),
		"views":                views,
		"searches":             searches,
		"series":               series,
		"topPages":             top,
	}
}

// AnalyticsLivePageView renders one live page view.
func AnalyticsLivePageView(e analyticsdomain.LiveEvent) gin.H {
	return gin.H{"ts": e.At.UTC().Format(time.RFC3339), "path": e.Path, "country": e.Country, "device": e.Device}
}

// AnalyticsLiveSearch renders one live search.
func AnalyticsLiveSearch(e analyticsdomain.LiveEvent) gin.H {
	return gin.H{"ts": e.At.UTC().Format(time.RFC3339), "query": e.Query, "resultCount": e.Results}
}

func searchQueries(rows []analyticsdomain.QueryRow, grain analyticsdomain.Grain, periods int) []gin.H {
	out := make([]gin.H, 0, len(rows))
	for _, q := range rows {
		item := gin.H{
			"query": q.Query, "searches": q.Searches, "zeroResults": q.ZeroResults, "clicks": q.Clicks,
			"ctr": ratio(q.SearchesWithClick, q.Searches), "avgSecondsToClick": ratio(q.ClickSeconds, q.SearchesWithClick),
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
