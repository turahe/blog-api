package persistence

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
	"github.com/turahe/blog-api/internal/platform/system"
	"gorm.io/gorm"
)

// rollupDay is far from today so no other test's raw events share its periods.
var rollupDay = time.Date(2021, 3, 10, 0, 0, 0, 0, time.UTC)

type rollupFixture struct {
	subject uuid.UUID
	search  uuid.UUID
}

// seedRollupDay stores two sessions on rollupDay: a consented visitor reading /a then /b and
// searching "go" (clicked after 10s), and an anonymous visitor bouncing on /a from a link.
// Both also search "go" and, without results, "nothing". The consented visitor returns the
// next day.
func seedRollupDay(t *testing.T, tx *gorm.DB) rollupFixture {
	t.Helper()

	stored, err := consentservice.New(NewConsentRepository(tx), system.UUIDGenerator{}, system.Clock{}).
		Store(t.Context(), "", nil, map[consentdomain.Purpose]bool{consentdomain.PurposeAnalytics: true}, "2026-09")
	require.NoError(t, err)

	subject := stored.Subject.UUID
	known := analyticsdomain.Visitor{SubjectUUID: &subject, Hash: strings.Repeat("a", 64), SessionID: uuid.New()}
	anon := analyticsdomain.Visitor{Hash: strings.Repeat("b", 64), SessionID: uuid.New()}
	at := func(h, m, s int) time.Time {
		return rollupDay.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	view := func(v analyticsdomain.Visitor, path, referrer string, when time.Time) analyticsdomain.Event {
		return analyticsdomain.Event{
			Kind: analyticsdomain.KindPageView, UUID: uuid.New(), Visitor: v, OccurredAt: when,
			PageView: &analyticsdomain.PageView{Path: path, Referrer: referrer, Device: analyticsdomain.DeviceDesktop, Browser: "firefox"},
		}
	}
	other := func(kind analyticsdomain.Kind, v analyticsdomain.Visitor, when time.Time) analyticsdomain.Event {
		return analyticsdomain.Event{Kind: kind, UUID: uuid.New(), Visitor: v, OccurredAt: when}
	}
	search := uuid.New()

	spent := other(analyticsdomain.KindTimeSpent, known, at(10, 0, 30))
	spent.TimeSpent = &analyticsdomain.TimeSpent{Path: "/a", FocusSeconds: 30}
	entry := other(analyticsdomain.KindNavigation, known, at(10, 0, 0))
	entry.Navigation = &analyticsdomain.Navigation{To: "/a", Transition: analyticsdomain.TransitionDirect}
	onward := other(analyticsdomain.KindNavigation, known, at(10, 1, 0))
	onward.Navigation = &analyticsdomain.Navigation{From: "/a", To: "/b", Transition: analyticsdomain.TransitionInternal}
	found := other(analyticsdomain.KindSearch, known, at(10, 2, 0))
	found.UUID, found.Search = search, &analyticsdomain.Search{Query: "go", ResultCount: 3}
	click := other(analyticsdomain.KindSearchClick, known, at(10, 2, 10))
	click.SearchClick = &analyticsdomain.SearchClick{
		SearchUUID: search, Position: 2, ResourceType: analyticsdomain.ResourcePost, ResourceUUID: uuid.New(),
	}
	empty := other(analyticsdomain.KindSearch, anon, at(11, 1, 0))
	empty.Search = &analyticsdomain.Search{Query: "nothing", ResultCount: 0}
	emptyToo := other(analyticsdomain.KindSearch, known, at(10, 3, 0))
	emptyToo.Search = &analyticsdomain.Search{Query: "nothing", ResultCount: 0}
	foundToo := other(analyticsdomain.KindSearch, anon, at(11, 0, 30))
	foundToo.Search = &analyticsdomain.Search{Query: "go", ResultCount: 3}

	events := []analyticsdomain.Event{
		view(known, "/a", "", at(10, 0, 0)),
		view(known, "/b", "", at(10, 1, 0)),
		view(anon, "/a", "https://news.example/story", at(11, 0, 0)),
		view(known, "/a", "", at(34, 0, 0)),
		spent, entry, onward, found, click, empty, emptyToo, foundToo,
	}
	require.NoError(t, NewAnalyticsRepository(tx).InsertBatch(t.Context(), events))

	return rollupFixture{subject: subject, search: search}
}

type rollupRow struct {
	Key                                              string
	Views, Visitors, Entries, Exits, FocusSeconds    int64
	Searches, ZeroResults, SearchesWithClick, Clicks int64
	ClickSeconds                                     int64
}

func rollupRows(t *testing.T, tx *gorm.DB, query string) map[string]rollupRow {
	t.Helper()

	var rows []rollupRow
	require.NoError(t, tx.Raw(query, rollupDay.Format(time.DateOnly)).Scan(&rows).Error)

	out := make(map[string]rollupRow, len(rows))
	for _, row := range rows {
		out[row.Key] = row
	}

	return out
}

func TestAnalyticsRollupsSummariseADay(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	seedRollupDay(t, tx)

	repo := NewAnalyticsRollupRepository(tx)
	day := analyticsdomain.PeriodOf(analyticsdomain.GrainDay, rollupDay, time.UTC)
	require.NoError(t, repo.RefreshFirstSeen(ctx, day.From, day.To.AddDate(0, 0, 1)))
	require.NoError(t, repo.RecomputePeriod(ctx, day, 1, time.Now()))
	require.NoError(t, repo.RecomputePeriod(ctx, day, 1, time.Now()), "recomputing replaces the period")

	var site struct {
		Views, Visitors, Sessions, Bounces, FocusSeconds, FocusViews               int64
		Searches, ZeroResultSearches, SearchesWithClick, SearchClicks, NewVisitors int64
		ConsentedVisitors                                                          int64
	}
	require.NoError(t, tx.Raw(`SELECT * FROM analytics_rollup_site WHERE grain = 'day' AND period_start = ?`,
		day.Day()).Scan(&site).Error)
	assert.Equal(t, int64(3), site.Views)
	assert.Equal(t, int64(2), site.Visitors)
	assert.Equal(t, int64(2), site.Sessions)
	assert.Equal(t, int64(1), site.Bounces)
	assert.Equal(t, int64(30), site.FocusSeconds)
	assert.Equal(t, int64(1), site.FocusViews)
	assert.Equal(t, int64(4), site.Searches)
	assert.Equal(t, int64(2), site.ZeroResultSearches)
	assert.Equal(t, int64(1), site.SearchesWithClick)
	assert.Equal(t, int64(1), site.SearchClicks)
	assert.Equal(t, int64(1), site.ConsentedVisitors)
	assert.Equal(t, int64(1), site.NewVisitors)

	pages := rollupRows(t, tx, `SELECT path AS key, views, visitors, entries, exits, focus_seconds
FROM analytics_rollup_pages WHERE grain = 'day' AND period_start = ?`)
	assert.Equal(t, map[string]rollupRow{
		"/a":      {Key: "/a", Views: 2, Visitors: 2, Entries: 2, Exits: 1, FocusSeconds: 30},
		"(other)": {Key: "(other)", Views: 1, Visitors: 1, Exits: 1},
	}, pages, "with a cap of 1 the less viewed /b folds into (other)")

	referrers := rollupRows(t, tx, `SELECT host AS key, sessions AS views FROM analytics_rollup_referrers
WHERE grain = 'day' AND period_start = ?`)
	assert.Equal(t, int64(1), referrers["(direct)"].Views)
	assert.Equal(t, int64(1), referrers["(other)"].Views, "news.example ranks second and is folded")

	searches := rollupRows(t, tx, `SELECT query AS key, searches, zero_results, searches_with_click, clicks, click_seconds
FROM analytics_rollup_searches WHERE grain = 'day' AND period_start = ?`)
	assert.Equal(t, rollupRow{Key: "go", Searches: 2, SearchesWithClick: 1, Clicks: 1, ClickSeconds: 10}, searches["go"])
	assert.Equal(t, rollupRow{Key: "(other)", Searches: 2, ZeroResults: 2}, searches["(other)"], "with a cap of 1 \"nothing\" folds")

	assert.Equal(t, int64(2), countWhere(t, tx, "analytics_rollup_navigation", "grain = 'day' AND period_start = ?", day.Day()))
	assert.Equal(t, int64(1), countWhere(t, tx, "analytics_rollup_search_positions", "period_start = ? AND position = 2", day.Day()))
	assert.Equal(t, int64(1), countWhere(t, tx, "analytics_rollup_search_results", "period_start = ? AND query = 'go'", day.Day()))
	assert.Equal(t, int64(3), countWhere(t, tx, "analytics_rollup_dimensions", "period_start = ?", day.Day()),
		"one country, device, and browser")
}

func TestAnalyticsRollupWeekCountsDistinctVisitors(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	seedRollupDay(t, tx)

	repo := NewAnalyticsRollupRepository(tx)
	week := analyticsdomain.PeriodOf(analyticsdomain.GrainWeek, rollupDay, time.UTC)
	require.NoError(t, repo.RecomputePeriod(ctx, week, analyticsdomain.RollupTopN, time.Now()))

	var site struct{ Views, Visitors int64 }
	require.NoError(t, tx.Raw(`SELECT views, visitors FROM analytics_rollup_site WHERE grain = 'week' AND period_start = ?`,
		week.Day()).Scan(&site).Error)
	assert.Equal(t, int64(4), site.Views, "the return visit the next day is in the same week")
	assert.Equal(t, int64(2), site.Visitors, "the consented visitor is counted once across both days")
}

func TestAnalyticsRollupCohorts(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	fixture := seedRollupDay(t, tx)

	repo := NewAnalyticsRollupRepository(tx)
	require.NoError(t, repo.RefreshFirstSeen(ctx, rollupDay, rollupDay.AddDate(0, 0, 2)))

	var first time.Time
	require.NoError(t, tx.Raw(`SELECT first_seen_at FROM analytics_subject_first_seen WHERE subject_uuid = ?`,
		fixture.subject).Scan(&first).Error)
	assert.True(t, first.Equal(rollupDay.Add(10*time.Hour)), "the first page view, not the return visit")

	require.NoError(t, repo.RecomputeCohort(ctx, analyticsdomain.CohortOf(rollupDay, time.UTC, time.Now()), time.Now()))

	var cohort struct {
		Size              int64
		Day1, Day7, Day30 *int64
	}
	require.NoError(t, tx.Raw(`SELECT size, day1, day7, day30 FROM analytics_rollup_cohorts WHERE cohort_day = ?`,
		rollupDay.Format(time.DateOnly)).Scan(&cohort).Error)
	assert.Equal(t, int64(1), cohort.Size)
	require.NotNil(t, cohort.Day1)
	assert.Equal(t, int64(1), *cohort.Day1)
	require.NotNil(t, cohort.Day30)
	assert.Zero(t, *cohort.Day30)

	open := analyticsdomain.CohortOf(rollupDay, time.UTC, rollupDay.AddDate(0, 0, 3))
	require.NoError(t, repo.RecomputeCohort(ctx, open, time.Now()))
	require.NoError(t, tx.Raw(`SELECT size, day1, day7, day30 FROM analytics_rollup_cohorts WHERE cohort_day = ?`,
		rollupDay.Format(time.DateOnly)).Scan(&cohort).Error)
	assert.Nil(t, cohort.Day7, "a return day that has not ended stays NULL")

	require.NoError(t, repo.DeleteFrom(ctx, rollupDay.Format(time.DateOnly)))
	assert.Zero(t, countWhere(t, tx, "analytics_rollup_cohorts", "cohort_day = ?", rollupDay.Format(time.DateOnly)))
}

// TestAnalyticsRollupsKeepOnlySharedQueries checks that a query is kept by name only when
// MinQueryVisitors distinct visitors searched it on one day.
func TestAnalyticsRollupsKeepOnlySharedQueries(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	monday := time.Date(2021, 4, 5, 9, 0, 0, 0, time.UTC)

	visitor := func(hash string) analyticsdomain.Visitor {
		return analyticsdomain.Visitor{Hash: strings.Repeat(hash, 64), SessionID: uuid.New()}
	}
	search := func(v analyticsdomain.Visitor, query string, when time.Time) analyticsdomain.Event {
		return analyticsdomain.Event{
			Kind: analyticsdomain.KindSearch, UUID: uuid.New(), Visitor: v, OccurredAt: when,
			Search: &analyticsdomain.Search{Query: query, ResultCount: 1},
		}
	}

	a, b, c := visitor("a"), visitor("b"), visitor("c")
	lone := search(a, "jane doe 555-0100", monday)
	events := []analyticsdomain.Event{
		search(a, "golang", monday), search(b, "golang", monday.Add(time.Hour)),
		lone, search(a, "jane doe 555-0100", monday.Add(time.Hour)),
		search(a, "spread", monday), search(c, "spread", monday.AddDate(0, 0, 1)),
		{
			Kind: analyticsdomain.KindSearchClick, UUID: uuid.New(), Visitor: a, OccurredAt: monday.Add(time.Minute),
			SearchClick: &analyticsdomain.SearchClick{
				SearchUUID: lone.UUID, Position: 1, ResourceType: analyticsdomain.ResourcePost, ResourceUUID: uuid.New(),
			},
		},
	}
	require.NoError(t, NewAnalyticsRepository(tx).InsertBatch(ctx, events))

	repo := NewAnalyticsRollupRepository(tx)
	week := analyticsdomain.PeriodOf(analyticsdomain.GrainWeek, monday, time.UTC)
	require.NoError(t, repo.RecomputePeriod(ctx, week, analyticsdomain.RollupTopN, time.Now()))

	var queries []struct {
		Query    string
		Searches int64
	}
	require.NoError(t, tx.Raw(`SELECT query, searches FROM analytics_rollup_searches WHERE grain = 'week' AND period_start = ?
		ORDER BY query`, week.Day()).Scan(&queries).Error)
	require.Len(t, queries, 2)
	assert.Equal(t, analyticsdomain.Other, queries[0].Query)
	assert.Equal(t, int64(4), queries[0].Searches, "one visitor's query, and one shared only across days, fold")
	assert.Equal(t, "golang", queries[1].Query)
	assert.Equal(t, int64(2), queries[1].Searches)

	assert.Zero(t, countWhere(t, tx, "analytics_rollup_search_results", "grain = 'week' AND period_start = ?", week.Day()),
		"clicked results of a folded query are not kept")
	assert.Equal(t, int64(1), countWhere(t, tx, "analytics_rollup_search_positions", "grain = 'week' AND period_start = ?",
		week.Day()), "positions carry no query")
}

type fixedZone string

func (z fixedZone) Timezone(context.Context) (string, error) { return string(z), nil }

func TestAnalyticsAggregatorBuildsRollupsFromRawEvents(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	now := time.Now().UTC()
	visitor := analyticsVisitor(nil)
	require.NoError(t, NewAnalyticsRepository(tx).InsertBatch(ctx, []analyticsdomain.Event{{
		Kind: analyticsdomain.KindPageView, UUID: uuid.New(), Visitor: visitor, OccurredAt: now,
		PageView: &analyticsdomain.PageView{Path: "/", Device: analyticsdomain.DeviceDesktop, Browser: "chrome"},
	}}))

	agg := analyticsservice.NewAggregator(NewAnalyticsRollupRepository(tx), fixedZone("UTC"), system.Clock{})

	result, err := agg.Run(ctx)
	require.NoError(t, err)
	assert.True(t, result.Rebuilt, "the first run builds every period")

	today := analyticsdomain.PeriodOf(analyticsdomain.GrainDay, now, time.UTC).Day()

	var views int64
	require.NoError(t, tx.Raw(`SELECT views FROM analytics_rollup_site WHERE grain = 'day' AND period_start = ?`, today).
		Scan(&views).Error)
	assert.Equal(t, int64(1), views)

	result, err = agg.Run(ctx)
	require.NoError(t, err)
	assert.False(t, result.Rebuilt)
	assert.GreaterOrEqual(t, result.Periods, 4, "today, yesterday, and their week and month")
}

func TestAnalyticsRollupState(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRollupRepository(tx)

	require.NoError(t, repo.SetTimezone(t.Context(), "Asia/Jakarta", time.Now()))
	zone, found, err := repo.Timezone(t.Context())
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "Asia/Jakarta", zone)

	seedRollupDay(t, tx)
	earliest, ok, err := repo.EarliestEvent(t.Context())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, earliest.After(rollupDay.Add(10*time.Hour)))
}
