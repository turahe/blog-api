package persistence

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AnalyticsRetentionRepository deletes expired analytics events and rollups.
type AnalyticsRetentionRepository struct {
	db *gorm.DB
}

// NewAnalyticsRetentionRepository returns an AnalyticsRetentionRepository over db.
func NewAnalyticsRetentionRepository(db *gorm.DB) *AnalyticsRetentionRepository {
	return &AnalyticsRetentionRepository{db: db}
}

// rawAnalyticsTables maps each raw event table to its indexed event time column.
var rawAnalyticsTables = []struct{ table, column string }{
	{"analytics_page_views", "occurred_at"},
	{"analytics_time_spent", "started_at"},
	{"analytics_navigation", "occurred_at"},
	{"analytics_searches", "occurred_at"},
	{"analytics_search_clicks", "occurred_at"},
}

// dayRollupTables are the rollup tables holding a row per grain and period.
var dayRollupTables = []string{
	"analytics_rollup_site", "analytics_rollup_pages", "analytics_rollup_referrers", "analytics_rollup_dimensions",
	"analytics_rollup_navigation", "analytics_rollup_searches", "analytics_rollup_search_positions",
	"analytics_rollup_search_results",
}

// PruneRaw deletes up to limit rows of each raw table stored before before.
func (r *AnalyticsRetentionRepository) PruneRaw(ctx context.Context, before time.Time, limit int) (int64, error) {
	var deleted int64

	for _, raw := range rawAnalyticsTables {
		result := conn(ctx, r.db).Exec(`DELETE FROM `+raw.table+` WHERE id IN (
			SELECT id FROM `+raw.table+` WHERE `+raw.column+` < ? LIMIT ?)`, before, limit)
		if result.Error != nil {
			return deleted, fmt.Errorf("prune %s: %w", raw.table, result.Error)
		}

		deleted += result.RowsAffected
	}

	return deleted, nil
}

// PruneDayRollups deletes daily rollups of periods starting before the local date day.
func (r *AnalyticsRetentionRepository) PruneDayRollups(ctx context.Context, day string) (int64, error) {
	var deleted int64

	for _, table := range dayRollupTables {
		result := conn(ctx, r.db).Exec(`DELETE FROM `+table+` WHERE grain = 'day' AND period_start < CAST(? AS date)`, day)
		if result.Error != nil {
			return deleted, fmt.Errorf("prune %s: %w", table, result.Error)
		}

		deleted += result.RowsAffected
	}

	return deleted, nil
}
