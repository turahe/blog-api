package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsports "github.com/turahe/blog-api/internal/core/analytics/ports"
	"gorm.io/gorm"
)

// analyticsRowsPerStatement keeps multi-row inserts well under PostgreSQL's parameter limit.
const analyticsRowsPerStatement = 1000

// AnalyticsRepository stores raw analytics events with multi-row inserts.
type AnalyticsRepository struct {
	db *gorm.DB
}

var _ analyticsports.EventRepository = (*AnalyticsRepository)(nil)

// NewAnalyticsRepository returns an AnalyticsRepository.
func NewAnalyticsRepository(db *gorm.DB) *AnalyticsRepository {
	return &AnalyticsRepository{db: db}
}

type analyticsTable struct {
	name     string
	columns  []string
	conflict string
	rows     [][]any
}

// InsertBatch writes events grouped by kind. Events already stored (same UUID) are ignored,
// and time-spent heartbeats keep the highest focus time and latest heartbeat per page view.
func (r *AnalyticsRepository) InsertBatch(ctx context.Context, events []analyticsdomain.Event) error {
	tables, err := analyticsTables(events)
	if err != nil {
		return err
	}

	db := conn(ctx, r.db)

	var errs []error

	for _, table := range tables {
		for start := 0; start < len(table.rows); start += analyticsRowsPerStatement {
			chunk := table.rows[start:min(start+analyticsRowsPerStatement, len(table.rows))]
			if err := insertAnalyticsRows(db, table, chunk); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func insertAnalyticsRows(db *gorm.DB, table analyticsTable, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	row := "(" + strings.TrimSuffix(strings.Repeat("?,", len(table.columns)), ",") + ")"
	values := make([]string, len(rows))
	args := make([]any, 0, len(rows)*len(table.columns))

	for i, r := range rows {
		values[i] = row

		args = append(args, r...)
	}

	query := "INSERT INTO " + table.name + " (" + strings.Join(table.columns, ", ") + ") VALUES " +
		strings.Join(values, ", ") + " " + table.conflict

	if err := db.Exec(query, args...).Error; err != nil {
		return fmt.Errorf("insert %s: %w", table.name, err)
	}

	return nil
}

// analyticsTables turns events into rows per table.
func analyticsTables(events []analyticsdomain.Event) ([]analyticsTable, error) {
	visitor := []string{"uuid", "subject_uuid", "visitor_hash", "session_id"}
	ignore := "ON CONFLICT (uuid) DO NOTHING"

	pageViews := analyticsTable{name: "analytics_page_views", conflict: ignore, columns: slices.Concat(visitor,
		[]string{"path", "referrer", "country_code", "device_type", "browser", "occurred_at"})}
	navigation := analyticsTable{name: "analytics_navigation", conflict: ignore, columns: slices.Concat(visitor,
		[]string{"from_path", "to_path", "transition_type", "occurred_at"})}
	searches := analyticsTable{name: "analytics_searches", conflict: ignore, columns: slices.Concat(visitor,
		[]string{"query", "result_count", "filters", "occurred_at"})}
	clicks := analyticsTable{name: "analytics_search_clicks", conflict: ignore, columns: slices.Concat(visitor,
		[]string{"search_uuid", "position", "resource_type", "resource_uuid", "occurred_at"})}

	for _, e := range events {
		base := []any{e.UUID, e.Visitor.SubjectUUID, e.Visitor.Hash, e.Visitor.SessionID}

		switch {
		case e.PageView != nil:
			v := e.PageView
			pageViews.rows = append(pageViews.rows, append(base,
				v.Path, nullableString(v.Referrer), nullableString(v.Country), string(v.Device), v.Browser, e.OccurredAt))
		case e.Navigation != nil:
			v := e.Navigation
			navigation.rows = append(navigation.rows, append(base,
				nullableString(v.From), v.To, string(v.Transition), e.OccurredAt))
		case e.Search != nil:
			filters, err := json.Marshal(e.Search.Filters)
			if err != nil {
				return nil, fmt.Errorf("encode search filters: %w", err)
			}

			searches.rows = append(searches.rows, append(base,
				e.Search.Query, e.Search.ResultCount, string(filters), e.OccurredAt))
		case e.SearchClick != nil:
			v := e.SearchClick
			clicks.rows = append(clicks.rows, append(base,
				v.SearchUUID, v.Position, string(v.ResourceType), v.ResourceUUID, e.OccurredAt))
		}
	}

	return []analyticsTable{pageViews, timeSpentTable(events), navigation, searches, clicks}, nil
}

// timeSpentTable folds heartbeats for the same page view into one row: an upsert cannot
// touch a row twice in one statement.
func timeSpentTable(events []analyticsdomain.Event) analyticsTable {
	type view struct {
		event     analyticsdomain.Event
		focus     int
		startedAt time.Time
		lastSeen  time.Time
	}

	views := map[uuid.UUID]*view{}
	order := []uuid.UUID{}

	for _, e := range events {
		if e.TimeSpent == nil {
			continue
		}

		v, ok := views[e.UUID]
		if !ok {
			v = &view{event: e, startedAt: e.OccurredAt, lastSeen: e.OccurredAt}
			views[e.UUID] = v
			order = append(order, e.UUID)
		}

		v.focus = max(v.focus, e.TimeSpent.FocusSeconds)
		v.startedAt = minTime(v.startedAt, e.OccurredAt)
		v.lastSeen = maxTime(v.lastSeen, e.OccurredAt)
	}

	table := analyticsTable{
		name: "analytics_time_spent",
		columns: []string{
			"uuid", "subject_uuid", "visitor_hash", "session_id", "path", "focus_seconds", "started_at", "last_seen_at",
		},
		conflict: "ON CONFLICT (uuid) DO UPDATE SET " +
			"focus_seconds = GREATEST(analytics_time_spent.focus_seconds, EXCLUDED.focus_seconds), " +
			"last_seen_at = GREATEST(analytics_time_spent.last_seen_at, EXCLUDED.last_seen_at)",
	}

	for _, id := range order {
		v := views[id]
		table.rows = append(table.rows, []any{
			id, v.event.Visitor.SubjectUUID, v.event.Visitor.Hash, v.event.Visitor.SessionID,
			v.event.TimeSpent.Path, v.focus, v.startedAt, v.lastSeen,
		})
	}

	return table
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
