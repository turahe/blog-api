package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
	"gorm.io/gorm"
)

// AnalyticsExportRepository stores rollup export requests and reads the rollups they export.
type AnalyticsExportRepository struct {
	db *gorm.DB
}

// NewAnalyticsExportRepository returns an AnalyticsExportRepository over db.
func NewAnalyticsExportRepository(db *gorm.DB) *AnalyticsExportRepository {
	return &AnalyticsExportRepository{db: db}
}

type analyticsExportRow struct {
	UUID        uuid.UUID
	UserUUID    uuid.UUID
	Grain       string
	FirstDay    string
	LastDay     string
	Timezone    string
	Status      string
	StorageKey  *string
	SizeBytes   *int64
	ExpiresAt   *time.Time
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

const analyticsExportColumns = `e.uuid, (SELECT u.uuid FROM users u WHERE u.id = e.user_id) AS user_uuid, e.grain,
	to_char(e.first_day, 'YYYY-MM-DD') AS first_day, to_char(e.last_day, 'YYYY-MM-DD') AS last_day, e.timezone,
	e.status, e.storage_key, e.size_bytes, e.expires_at, e.attempts, e.last_error, e.created_at, e.started_at,
	e.completed_at`

// Create inserts a pending export; a second open export of the same user is
// domain.ErrExportOpen. The insert runs in a savepoint so that conflict leaves an enclosing
// transaction usable.
func (r *AnalyticsExportRepository) Create(ctx context.Context, export domain.Export) error {
	err := conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		return tx.Exec(`
		INSERT INTO analytics_exports (uuid, user_id, grain, first_day, last_day, timezone, status, created_at)
		VALUES (?, `+idOf("users")+`, ?, CAST(? AS date), CAST(? AS date), ?, ?, ?)`,
			export.UUID, export.RequestedBy, string(export.Grain), export.FirstDay, export.LastDay, export.Timezone,
			string(export.Status), export.CreatedAt).Error
	})
	if isUniqueViolation(err) {
		return domain.ErrExportOpen
	}

	return err
}

// Get returns the user's export.
func (r *AnalyticsExportRepository) Get(ctx context.Context, id, userID uuid.UUID) (domain.Export, error) {
	return r.one(ctx, `SELECT `+analyticsExportColumns+` FROM analytics_exports e
		WHERE e.uuid = ? AND e.user_id = `+idOf("users"), id, userID)
}

// Open returns the user's pending or running export.
func (r *AnalyticsExportRepository) Open(ctx context.Context, userID uuid.UUID) (domain.Export, error) {
	return r.one(ctx, `SELECT `+analyticsExportColumns+` FROM analytics_exports e
		WHERE e.user_id = `+idOf("users")+` AND e.status IN ('pending', 'running')`, userID)
}

// List returns the user's most recent exports, newest first.
func (r *AnalyticsExportRepository) List(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Export, error) {
	return r.list(ctx, `SELECT `+analyticsExportColumns+` FROM analytics_exports e
		WHERE e.user_id = `+idOf("users")+` ORDER BY e.created_at DESC, e.id DESC LIMIT ?`, userID, limit)
}

// ClaimNext marks the oldest claimable export running. SKIP LOCKED lets several schedulers
// claim concurrently without taking the same export.
func (r *AnalyticsExportRepository) ClaimNext(ctx context.Context, now, staleBefore time.Time) (domain.Export, bool, error) {
	exports, err := r.list(ctx, `UPDATE analytics_exports e
		SET status = 'running', started_at = ?, attempts = e.attempts + 1
		WHERE e.id = (
			SELECT q.id FROM analytics_exports q
			WHERE q.status = 'pending' OR (q.status = 'running' AND q.started_at < ?)
			ORDER BY q.created_at, q.id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+analyticsExportColumns, now, staleBefore)
	if err != nil || len(exports) == 0 {
		return domain.Export{}, false, err
	}

	return exports[0], true, nil
}

// Complete marks the export completed with its archive.
func (r *AnalyticsExportRepository) Complete(
	ctx context.Context, id uuid.UUID, storageKey string, size int64, expiresAt, at time.Time,
) error {
	return r.exec(ctx, `UPDATE analytics_exports SET status = 'completed', storage_key = ?, size_bytes = ?,
		expires_at = ?, completed_at = ?, last_error = NULL WHERE uuid = ?`, storageKey, size, expiresAt, at, id)
}

// Fail records message and requeues the export, or marks it failed when final.
func (r *AnalyticsExportRepository) Fail(ctx context.Context, id uuid.UUID, message string, final bool, at time.Time) error {
	status, completed := string(domain.ExportPending), (*time.Time)(nil)
	if final {
		status, completed = string(domain.ExportFailed), &at
	}

	return r.exec(ctx, `UPDATE analytics_exports SET status = ?, last_error = ?, completed_at = ? WHERE uuid = ?`,
		status, message, completed, id)
}

// Archives returns exports whose archive expired before before, oldest expiry first.
func (r *AnalyticsExportRepository) Archives(ctx context.Context, before time.Time, limit int) ([]domain.Export, error) {
	return r.list(ctx, `SELECT `+analyticsExportColumns+` FROM analytics_exports e
		WHERE e.storage_key IS NOT NULL AND e.expires_at < ? ORDER BY e.expires_at, e.id LIMIT ?`, before, limit)
}

// ClearArchive forgets a deleted archive.
func (r *AnalyticsExportRepository) ClearArchive(ctx context.Context, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE analytics_exports SET storage_key = NULL WHERE uuid = ?`, id)
}

// exportTables are the rollup tables an export writes, in order. Each query binds grain,
// first, and last.
var exportTables = []struct{ name, query string }{
	{"site", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, views, visitors, sessions, bounces,
	focus_seconds, focus_views, searches, zero_result_searches, searches_with_click, search_clicks, consented_visitors,
	new_visitors FROM analytics_rollup_site WHERE ` + inWindow + ` ORDER BY period_start`},
	{"pages", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, path, views, visitors, entries, exits,
	focus_seconds, focus_views FROM analytics_rollup_pages WHERE ` + inWindow + ` ORDER BY period_start, views DESC, path`},
	{"referrers", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, host, sessions, visitors
	FROM analytics_rollup_referrers WHERE ` + inWindow + ` ORDER BY period_start, sessions DESC, host`},
	{"audience", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, dimension, value, views, visitors
	FROM analytics_rollup_dimensions WHERE ` + inWindow + ` ORDER BY period_start, dimension, views DESC, value`},
	{"navigation", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, from_path, to_path, transition,
	transitions FROM analytics_rollup_navigation WHERE ` + inWindow + `
	ORDER BY period_start, transitions DESC, from_path, to_path, transition`},
	{"searches", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, query, searches, visitors, zero_results,
	searches_with_click, clicks, click_seconds FROM analytics_rollup_searches WHERE ` + inWindow + `
	ORDER BY period_start, searches DESC, query`},
	{"search_positions", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, position, clicks
	FROM analytics_rollup_search_positions WHERE ` + inWindow + ` ORDER BY period_start, position`},
	{"search_results", `SELECT to_char(period_start, 'YYYY-MM-DD') AS period_start, query, resource_type,
	CAST(resource_uuid AS text) AS resource_uuid, clicks FROM analytics_rollup_search_results WHERE ` + inWindow + `
	ORDER BY period_start, clicks DESC, query, resource_type, resource_uuid`},
}

const exportCohorts = `SELECT to_char(cohort_day, 'YYYY-MM-DD') AS cohort_day, size, day1, day7, day30
FROM analytics_rollup_cohorts WHERE cohort_day BETWEEN CAST(? AS date) AND CAST(? AS date) ORDER BY cohort_day`

// ExportRollups writes every rollup table's rows of sel, then the cohorts of the local days
// first through last.
func (r *AnalyticsExportRepository) ExportRollups(
	ctx context.Context, sel domain.Selection, first, last string, w ports.TableWriter,
) error {
	for _, table := range exportTables {
		if err := r.writeTable(ctx, w, table.name, table.query, selArgs(sel)...); err != nil {
			return err
		}
	}

	return r.writeTable(ctx, w, "cohorts", exportCohorts, first, last)
}

func (r *AnalyticsExportRepository) writeTable(ctx context.Context, w ports.TableWriter, name, query string, args ...any) error {
	rows, err := conn(ctx, r.db).Raw(query, args...).Rows()
	if err != nil {
		return fmt.Errorf("export %s: %w", name, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	if err := w.Table(name, columns); err != nil {
		return err
	}

	cells := make([]sql.NullString, len(columns))
	dest := make([]any, len(columns))

	for i := range cells {
		dest[i] = &cells[i]
	}

	values := make([]string, len(columns))

	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("export %s: %w", name, err)
		}

		for i, cell := range cells {
			values[i] = cell.String
		}

		if err := w.Row(values); err != nil {
			return err
		}
	}

	return rows.Err()
}

func (r *AnalyticsExportRepository) exec(ctx context.Context, query string, args ...any) error {
	result := conn(ctx, r.db).Exec(query, args...)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return domain.ErrExportNotFound
	}

	return nil
}

func (r *AnalyticsExportRepository) one(ctx context.Context, query string, args ...any) (domain.Export, error) {
	exports, err := r.list(ctx, query, args...)
	if err != nil {
		return domain.Export{}, err
	}

	if len(exports) == 0 {
		return domain.Export{}, domain.ErrExportNotFound
	}

	return exports[0], nil
}

func (r *AnalyticsExportRepository) list(ctx context.Context, query string, args ...any) ([]domain.Export, error) {
	var rows []analyticsExportRow
	if err := conn(ctx, r.db).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("analytics exports: %w", err)
	}

	out := make([]domain.Export, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Export{
			UUID: row.UUID, RequestedBy: row.UserUUID, Grain: domain.Grain(row.Grain),
			FirstDay: row.FirstDay, LastDay: row.LastDay, Timezone: row.Timezone, Status: domain.ExportStatus(row.Status),
			StorageKey: row.StorageKey, SizeBytes: row.SizeBytes, ExpiresAt: row.ExpiresAt, Attempts: row.Attempts,
			LastError: row.LastError, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt,
		})
	}

	return out, nil
}
