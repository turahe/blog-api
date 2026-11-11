package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
	"gorm.io/gorm"
)

var settingsHistoryCfg = pagination.CursorConfig{
	Kind: "settings_history",
	Sort: []pagination.SortField{
		{Name: "created_at", Column: "h.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "h.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	DefaultPerPage: 20,
	MaxPerPage:     100,
}

// SettingsRepository stores changed settings and their history in PostgreSQL.
type SettingsRepository struct {
	db *gorm.DB
}

// NewSettingsRepository returns a SettingsRepository over db.
func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

type settingRow struct {
	Key       string
	Value     string
	Version   int64
	UpdatedAt time.Time
	UpdatedBy *uuid.UUID
}

const settingSelect = `
	SELECT s.key, s.value::text AS value, s.version, s.updated_at, u.uuid AS updated_by
	FROM settings s LEFT JOIN users u ON u.id = s.updated_by`

// List implements ports.Repository.
func (r *SettingsRepository) List(ctx context.Context) ([]settingsdomain.Stored, error) {
	var rows []settingRow
	if err := conn(ctx, r.db).Raw(settingSelect + ` ORDER BY s.key`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}

	return storedSettings(rows), nil
}

// Lock implements ports.Repository. Rows that do not exist yet cannot be locked; Save's
// insert detects a concurrent first write instead.
func (r *SettingsRepository) Lock(ctx context.Context, keys []string) ([]settingsdomain.Stored, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	var rows []settingRow
	if err := conn(ctx, r.db).Raw(settingSelect+` WHERE s.key IN ? ORDER BY s.key FOR UPDATE OF s`, keys).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lock settings: %w", err)
	}

	return storedSettings(rows), nil
}

// Save implements ports.Repository.
func (r *SettingsRepository) Save(ctx context.Context, w settingsdomain.Write) error {
	c := conn(ctx, r.db)

	var res *gorm.DB
	if w.ExpectedVersion == 0 {
		res = c.Exec(`
			INSERT INTO settings (key, value, version, updated_by, created_at, updated_at)
			VALUES (?, ?::jsonb, 1, (SELECT id FROM users WHERE uuid = ?), ?, ?)
			ON CONFLICT (key) DO NOTHING`,
			w.Key, string(w.Value), w.ChangedBy, w.At, w.At)
	} else {
		res = c.Exec(`
			UPDATE settings SET value = ?::jsonb, version = version + 1,
				updated_by = (SELECT id FROM users WHERE uuid = ?), updated_at = ?
			WHERE key = ? AND version = ?`,
			string(w.Value), w.ChangedBy, w.At, w.Key, w.ExpectedVersion)
	}

	if res.Error != nil {
		return fmt.Errorf("save setting %s: %w", w.Key, res.Error)
	}

	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: %s changed concurrently", settingsdomain.ErrVersionConflict, w.Key)
	}

	var previous any
	if len(w.Previous) > 0 {
		previous = string(w.Previous)
	}

	if err := c.Exec(`
		INSERT INTO settings_history (uuid, setting_key, previous_value, new_value, version, changed_by, request_id, created_at)
		VALUES (?, ?, ?::jsonb, ?::jsonb, ?, (SELECT id FROM users WHERE uuid = ?), ?, ?)`,
		w.HistoryID, w.Key, previous, string(w.Value), w.ExpectedVersion+1, w.ChangedBy, w.RequestID, w.At).Error; err != nil {
		return fmt.Errorf("record setting history %s: %w", w.Key, err)
	}

	return nil
}

// settingsSeedRequestID marks history rows written by SeedDefaults.
const settingsSeedRequestID = "seed"

// SeedDefaults stores every catalogue key that has no row yet at its coded default, with
// a version-1 history row, and never overwrites a stored value.
func (r *SettingsRepository) SeedDefaults(ctx context.Context, catalogue settingsdomain.Catalogue, at time.Time) error {
	c := conn(ctx, r.db)

	for _, def := range catalogue.Definitions() {
		value, err := json.Marshal(def.Default)
		if err != nil {
			return fmt.Errorf("encode default of setting %s: %w", def.Key, err)
		}

		if err := c.Exec(`
			WITH seeded AS (
				INSERT INTO settings (key, value, version, created_at, updated_at)
				VALUES (?, ?::jsonb, 1, ?, ?)
				ON CONFLICT (key) DO NOTHING
				RETURNING key, value
			)
			INSERT INTO settings_history (setting_key, new_value, version, request_id, created_at)
			SELECT key, value, 1, ?, ? FROM seeded`,
			def.Key, string(value), at, at, settingsSeedRequestID, at).Error; err != nil {
			return fmt.Errorf("seed setting %s: %w", def.Key, err)
		}
	}

	return nil
}

type settingHistoryRow struct {
	UUID          uuid.UUID
	SettingKey    string
	PreviousValue *string
	NewValue      string
	Version       int64
	ChangedBy     *uuid.UUID
	RequestID     string
	CreatedAt     time.Time
}

// History implements ports.Repository.
func (r *SettingsRepository) History(ctx context.Context, filter settingsdomain.HistoryFilter) (settingsdomain.HistoryPage, error) {
	c := conn(ctx, r.db)
	pr := filter.PageRequest
	if pr.Limit <= 0 {
		pr.Limit = settingsHistoryCfg.DefaultPerPage
	}

	var cursorFields map[string]any
	if pr.Mode == pagination.ModeCursor && pr.Cursor != "" {
		decoded, _, err := pagination.DecodeCursor(settingsHistoryCfg, pr.Cursor)
		if err != nil {
			return settingsdomain.HistoryPage{}, err
		}
		if err := pagination.ValidateCursor(settingsHistoryCfg, decoded); err != nil {
			return settingsdomain.HistoryPage{}, err
		}
		cursorFields = decoded
	}
	seek, err := pagination.BuildSeek(settingsHistoryCfg, pr, cursorFields)
	if err != nil {
		return settingsdomain.HistoryPage{}, err
	}

	baseWhere := `(? = '' OR h.setting_key = ?)`
	whereArgs := []any{filter.Key, filter.Key}

	countSQL := fmt.Sprintf(`SELECT count(*) FROM settings_history h WHERE %s`, baseWhere)
	baseSQL := `
		SELECT h.id, h.uuid, h.setting_key, h.previous_value::text AS previous_value, h.new_value::text AS new_value,
			h.version, u.uuid AS changed_by, h.request_id, h.created_at
		FROM settings_history h LEFT JOIN users u ON u.id = h.changed_by
		WHERE ` + baseWhere

	var out settingsdomain.HistoryPage
	if pr.IncludeTotal {
		var total int64
		if err := c.Raw(countSQL, whereArgs...).Scan(&total).Error; err != nil {
			return out, fmt.Errorf("count settings history: %w", err)
		}
		out.Total = &total
	}

	querySQL := baseSQL
	if seek.WhereClause != "" {
		querySQL += ` AND ` + seek.WhereClause
		whereArgs = append(whereArgs, seek.BindVars...)
	}
	querySQL += ` ` + seek.OrderClause + ` LIMIT ?`
	queryArgs := append([]any{}, whereArgs...)
	queryArgs = append(queryArgs, seek.LimitFetch)
	if pr.Mode == pagination.ModeOffset {
		querySQL += ` OFFSET ?`
		queryArgs = append(queryArgs, pr.Offset)
	}

	var rawRows []struct {
		ID            int64
		UUID          uuid.UUID
		SettingKey    string
		PreviousValue *string
		NewValue      string
		Version       int64
		ChangedBy     *uuid.UUID
		RequestID     string
		CreatedAt     time.Time
	}
	if err := c.Raw(querySQL, queryArgs...).Scan(&rawRows).Error; err != nil {
		return out, fmt.Errorf("list settings history: %w", err)
	}

	if seek.ReverseDisplay {
		for i, j := 0, len(rawRows)-1; i < j; i, j = i+1, j-1 {
			rawRows[i], rawRows[j] = rawRows[j], rawRows[i]
		}
	}

	type rowKey struct {
		CreatedAt time.Time
		ID        int64
	}
	keys := make([]rowKey, 0, len(rawRows))
	items := make([]settingsdomain.HistoryEntry, 0, len(rawRows))
	for _, row := range rawRows {
		keys = append(keys, rowKey{CreatedAt: row.CreatedAt, ID: row.ID})
		entry := settingsdomain.HistoryEntry{
			UUID: row.UUID, Key: row.SettingKey, New: []byte(row.NewValue), Version: row.Version,
			ChangedBy: row.ChangedBy, RequestID: row.RequestID, CreatedAt: row.CreatedAt,
		}
		if row.PreviousValue != nil {
			entry.Previous = []byte(*row.PreviousValue)
		}
		items = append(items, entry)
	}

	page, hasNext, hasPrev := pagination.TruncatePage(items, pr.Limit, pr.Forward, pr.Cursor != "")
	out.Items = page
	out.HasNextPage = hasNext
	out.HasPreviousPage = hasPrev
	out.Limit = pr.Limit
	if pr.Mode == pagination.ModeOffset {
		out.OffsetPage = pr.Page
		out.OffsetPerPage = pr.Limit
	}

	if len(page) > 0 {
		var firstIdx, lastIdx int
		switch {
		case !pr.Forward && len(rawRows) > pr.Limit:
			firstIdx = 1
			lastIdx = pr.Limit
		default:
			firstIdx = 0
			lastIdx = len(page) - 1
		}
		if out.HasNextPage {
			k := keys[lastIdx]
			fields := pagination.SortValues[settingsdomain.HistoryEntry](settingsHistoryCfg, page[len(page)-1],
				func(_ settingsdomain.HistoryEntry, i int) any {
					switch i {
					case 0:
						return k.CreatedAt
					case 1:
						return k.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(settingsHistoryCfg, fields)
			if err != nil {
				return out, err
			}
			out.NextCursor = cur
		}
		if out.HasPreviousPage {
			k := keys[firstIdx]
			fields := pagination.SortValues[settingsdomain.HistoryEntry](settingsHistoryCfg, page[0],
				func(_ settingsdomain.HistoryEntry, i int) any {
					switch i {
					case 0:
						return k.CreatedAt
					case 1:
						return k.ID
					}
					return nil
				})
			cur, err := pagination.EncodeCursor(settingsHistoryCfg, fields)
			if err != nil {
				return out, err
			}
			out.PreviousCursor = cur
		}
	}

	return out, nil
}

func storedSettings(rows []settingRow) []settingsdomain.Stored {
	out := make([]settingsdomain.Stored, 0, len(rows))
	for _, row := range rows {
		out = append(out, settingsdomain.Stored{
			Key: row.Key, Value: []byte(row.Value), Version: row.Version,
			UpdatedAt: row.UpdatedAt, UpdatedBy: row.UpdatedBy,
		})
	}

	return out
}
