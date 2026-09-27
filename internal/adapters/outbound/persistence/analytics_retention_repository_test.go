package persistence

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ancientDay returns a random UTC midnight between the years 1200 and 1800: before every real
// row, and unlikely to collide with another run's keys.
func ancientDay() time.Time {
	return time.Date(1200+rand.IntN(600), time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, rand.IntN(365))
}

func insertRawAnalytics(t *testing.T, tx *gorm.DB, at time.Time) {
	t.Helper()

	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO analytics_page_views (uuid, visitor_hash, session_id, path, device_type, browser, occurred_at)
			VALUES (gen_random_uuid(), repeat('a', 64), gen_random_uuid(), '/', 'desktop', 'Firefox', ?)`, []any{at}},
		{`INSERT INTO analytics_time_spent (uuid, visitor_hash, session_id, path, focus_seconds, started_at, last_seen_at)
			VALUES (gen_random_uuid(), repeat('a', 64), gen_random_uuid(), '/', 5, ?, ?)`, []any{at, at}},
		{`INSERT INTO analytics_navigation (uuid, visitor_hash, session_id, to_path, transition_type, occurred_at)
			VALUES (gen_random_uuid(), repeat('a', 64), gen_random_uuid(), '/', 'direct', ?)`, []any{at}},
		{`INSERT INTO analytics_searches (uuid, visitor_hash, session_id, query, result_count, occurred_at)
			VALUES (gen_random_uuid(), repeat('a', 64), gen_random_uuid(), 'go', 1, ?)`, []any{at}},
		{`INSERT INTO analytics_search_clicks (uuid, search_uuid, visitor_hash, session_id, position, resource_type, resource_uuid, occurred_at)
			VALUES (gen_random_uuid(), gen_random_uuid(), repeat('a', 64), gen_random_uuid(), 1, 'post', gen_random_uuid(), ?)`, []any{at}},
	}

	for _, statement := range statements {
		require.NoError(t, tx.Exec(statement.sql, statement.args...).Error)
	}
}

func TestAnalyticsRetentionRepositoryPruneRaw(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRetentionRepository(tx)
	ctx := t.Context()
	day := ancientDay()

	insertRawAnalytics(t, tx, day)
	insertRawAnalytics(t, tx, day.Add(time.Hour))
	insertRawAnalytics(t, tx, day.Add(3*time.Hour))
	cutoff := day.Add(2 * time.Hour)

	deleted, err := repo.PruneRaw(ctx, cutoff, 1)
	require.NoError(t, err)
	require.Equal(t, int64(5), deleted, "at most limit rows per table")

	deleted, err = repo.PruneRaw(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, int64(5), deleted)

	deleted, err = repo.PruneRaw(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Zero(t, deleted, "rows after the cutoff stay")

	require.Equal(t, int64(1), countWhere(t, tx, "analytics_page_views", "occurred_at = ?", day.Add(3*time.Hour)))

	_, err = repo.PruneRaw(canceledContext(t), cutoff, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAnalyticsRetentionRepositoryPruneSalts(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRetentionRepository(tx)
	day := ancientDay()

	for _, d := range []time.Time{day, day.AddDate(0, 0, 1)} {
		require.NoError(t, tx.Exec(`INSERT INTO analytics_salts (day, salt) VALUES (?, decode(repeat('ab', 32), 'hex'))`,
			d.Format(time.DateOnly)).Error)
	}

	deleted, err := repo.PruneSalts(t.Context(), day.AddDate(0, 0, 1).Format(time.DateOnly))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Equal(t, int64(1), countWhere(t, tx, "analytics_salts", "day = ?", day.AddDate(0, 0, 1).Format(time.DateOnly)))
}

func TestAnalyticsRetentionRepositoryPruneDayRollups(t *testing.T) {
	t.Parallel()

	tx := integrationTx(t)
	repo := NewAnalyticsRetentionRepository(tx)
	day := ancientDay()

	insert := func(grain string, periodStart time.Time) {
		t.Helper()

		require.NoError(t, tx.Exec(`INSERT INTO analytics_rollup_site VALUES (?, ?, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, now())`,
			grain, periodStart.Format(time.DateOnly)).Error)
	}

	insert("day", day)
	insert("week", day)
	insert("day", day.AddDate(0, 0, 1))

	deleted, err := repo.PruneDayRollups(t.Context(), day.AddDate(0, 0, 1).Format(time.DateOnly))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "only daily rollups before the day")
	require.Equal(t, int64(2), countWhere(t, tx, "analytics_rollup_site", "period_start BETWEEN ? AND ?",
		day.Format(time.DateOnly), day.AddDate(0, 0, 1).Format(time.DateOnly)))

	_, err = repo.PruneDayRollups(canceledContext(t), day.Format(time.DateOnly))
	require.ErrorIs(t, err, context.Canceled)
}
