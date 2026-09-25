package persistence

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
)

func newAnalyticsExport(userID uuid.UUID, at time.Time) analyticsdomain.Export {
	return analyticsdomain.Export{
		UUID: uuid.New(), RequestedBy: userID, Grain: analyticsdomain.GrainWeek, FirstDay: "2026-08-03", LastDay: "2026-08-30",
		Timezone: "Asia/Jakarta", Status: analyticsdomain.ExportPending, CreatedAt: at,
	}
}

func TestAnalyticsExportRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	repo := NewAnalyticsExportRepository(tx)
	owner, other := insertUser(t, tx), insertUser(t, tx)
	now := time.Now().UTC().Truncate(time.Microsecond)

	export := newAnalyticsExport(owner, now.Add(-time.Hour))
	require.NoError(t, repo.Create(ctx, export))
	require.ErrorIs(t, repo.Create(ctx, newAnalyticsExport(owner, now)), analyticsdomain.ErrExportOpen)
	require.NoError(t, repo.Create(ctx, newAnalyticsExport(other, now)), "users queue independently")

	got, err := repo.Get(ctx, export.UUID, owner)
	require.NoError(t, err)
	assert.Equal(t, owner, got.RequestedBy)
	assert.Equal(t, analyticsdomain.GrainWeek, got.Grain)
	assert.Equal(t, "2026-08-03", got.FirstDay)
	assert.Equal(t, "2026-08-30", got.LastDay)
	assert.Equal(t, "Asia/Jakarta", got.Timezone)

	_, err = repo.Get(ctx, export.UUID, other)
	require.ErrorIs(t, err, analyticsdomain.ErrExportNotFound, "only the requester sees an export")

	open, err := repo.Open(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, export.UUID, open.UUID)

	claimed, ok, err := repo.ClaimNext(ctx, now, now.Add(-30*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, export.UUID, claimed.UUID, "oldest first")
	assert.Equal(t, analyticsdomain.ExportRunning, claimed.Status)
	assert.Equal(t, 1, claimed.Attempts)

	expires := now.Add(72 * time.Hour)
	require.NoError(t, repo.Complete(ctx, export.UUID, "analytics-exports/a.zip", 1234, expires, now))

	done, err := repo.Get(ctx, export.UUID, owner)
	require.NoError(t, err)
	assert.Equal(t, analyticsdomain.ExportCompleted, done.Status)
	assert.Equal(t, int64(1234), *done.SizeBytes)
	assert.True(t, done.Downloadable(now))

	_, err = repo.Open(ctx, owner)
	require.ErrorIs(t, err, analyticsdomain.ErrExportNotFound)

	archives, err := repo.Archives(ctx, now, 10)
	require.NoError(t, err)
	assert.Empty(t, archives, "not expired yet")

	archives, err = repo.Archives(ctx, expires.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, archives, 1)
	require.NoError(t, repo.ClearArchive(ctx, export.UUID))

	cleared, err := repo.Get(ctx, export.UUID, owner)
	require.NoError(t, err)
	assert.Nil(t, cleared.StorageKey)
	assert.Equal(t, analyticsdomain.ExportExpired, cleared.State(now))

	second := newAnalyticsExport(owner, now)
	require.NoError(t, repo.Create(ctx, second))
	require.NoError(t, repo.Fail(ctx, second.UUID, "boom", true, now))

	failed, err := repo.Get(ctx, second.UUID, owner)
	require.NoError(t, err)
	assert.Equal(t, analyticsdomain.ExportFailed, failed.Status)
	assert.Equal(t, "boom", *failed.LastError)

	list, err := repo.List(ctx, owner, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, second.UUID, list[0].UUID, "newest first")

	require.ErrorIs(t, repo.ClearArchive(ctx, uuid.New()), analyticsdomain.ErrExportNotFound)
}

type capturedTables struct {
	names   []string
	columns map[string][]string
	rows    map[string][][]string
	current string
}

func (c *capturedTables) Table(name string, columns []string) error {
	c.names, c.current = append(c.names, name), name
	c.columns[name] = columns

	return nil
}

func (c *capturedTables) Row(values []string) error {
	c.rows[c.current] = append(c.rows[c.current], append([]string(nil), values...))

	return nil
}

func TestAnalyticsExportRepositoryExportsRollups(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	seedReportRollups(t, tx)

	out := &capturedTables{columns: map[string][]string{}, rows: map[string][][]string{}}
	day := rollupDay.Format(time.DateOnly)
	next := rollupDay.AddDate(0, 0, 1).Format(time.DateOnly)
	sel := analyticsdomain.Selection{Grain: analyticsdomain.GrainDay, First: day, Last: next}

	require.NoError(t, NewAnalyticsExportRepository(tx).ExportRollups(t.Context(), sel, day, next, out))

	assert.Equal(t, []string{
		"site", "pages", "referrers", "audience", "navigation", "searches", "search_positions", "search_results", "cohorts",
	}, out.names)
	assert.Equal(t, []string{"period_start", "path", "views", "visitors", "entries", "exits", "focus_seconds", "focus_views"},
		out.columns["pages"])

	require.Len(t, out.rows["site"], 2)
	assert.Equal(t, day, out.rows["site"][0][0])
	assert.Equal(t, "3", out.rows["site"][0][1], "views")

	require.NotEmpty(t, out.rows["search_results"])
	_, err := uuid.Parse(out.rows["search_results"][0][3])
	require.NoError(t, err, "resource_uuid is text")

	require.NotEmpty(t, out.rows["cohorts"])
	assert.Equal(t, day, out.rows["cohorts"][0][0])

	for _, table := range out.names {
		assert.NotContains(t, out.columns[table], "visitor_hash")
		assert.NotContains(t, out.columns[table], "session_id")
		assert.NotContains(t, out.columns[table], "subject_uuid")
	}
}

func TestAnalyticsRetentionRepositoryPrunes(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	ctx := t.Context()
	seedReportRollups(t, tx)

	rollups := NewAnalyticsRollupRepository(tx)
	week := analyticsdomain.PeriodOf(analyticsdomain.GrainWeek, rollupDay, time.UTC)
	require.NoError(t, rollups.RecomputePeriod(ctx, week, analyticsdomain.RollupTopN, time.Now()))

	count := func(query string, args ...any) int64 {
		var n int64
		require.NoError(t, tx.Raw(query, args...).Scan(&n).Error)

		return n
	}
	rawBefore := count(`SELECT count(*) FROM analytics_page_views WHERE occurred_at < ?`, rollupDay.AddDate(0, 0, 2))
	require.Positive(t, rawBefore)

	repo := NewAnalyticsRetentionRepository(tx)

	deleted, err := repo.PruneRaw(ctx, rollupDay.Add(10*time.Hour+30*time.Minute), 1)
	require.NoError(t, err)
	assert.Positive(t, deleted)
	assert.LessOrEqual(t, deleted, int64(len(rawAnalyticsTables)), "at most limit rows per table")

	for {
		n, err := repo.PruneRaw(ctx, rollupDay.Add(10*time.Hour+30*time.Minute), 1000)
		require.NoError(t, err)

		if n == 0 {
			break
		}
	}

	assert.Zero(t, count(`SELECT count(*) FROM analytics_page_views WHERE occurred_at BETWEEN ? AND ?`,
		rollupDay, rollupDay.Add(10*time.Hour+30*time.Minute)))
	assert.Positive(t, count(`SELECT count(*) FROM analytics_searches WHERE occurred_at BETWEEN ? AND ?`,
		rollupDay.Add(10*time.Hour+30*time.Minute), rollupDay.AddDate(0, 0, 1)), "later events are kept")

	pruned, err := repo.PruneDayRollups(ctx, rollupDay.AddDate(0, 0, 1).Format(time.DateOnly))
	require.NoError(t, err)
	assert.Positive(t, pruned)

	day := rollupDay.Format(time.DateOnly)
	assert.Zero(t, count(`SELECT count(*) FROM analytics_rollup_site WHERE grain = 'day' AND period_start = CAST(? AS date)`, day))
	assert.Equal(t, int64(1), count(`SELECT count(*) FROM analytics_rollup_site WHERE grain = 'day' AND period_start = CAST(? AS date)`,
		rollupDay.AddDate(0, 0, 1).Format(time.DateOnly)))
	assert.Equal(t, int64(1), count(`SELECT count(*) FROM analytics_rollup_site WHERE grain = 'week' AND period_start = CAST(? AS date)`,
		week.Day()), "weekly rollups are kept")
	assert.Equal(t, int64(1), count(`SELECT count(*) FROM analytics_rollup_cohorts WHERE cohort_day = CAST(? AS date)`, day),
		"cohorts are kept")
}
