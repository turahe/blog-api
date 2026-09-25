package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	"gorm.io/gorm"
)

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
	page := settingsdomain.HistoryPage{Items: []settingsdomain.HistoryEntry{}, Page: filter.Page, PerPage: filter.PerPage}

	if err := c.Raw(`SELECT count(*) FROM settings_history WHERE (? = '' OR setting_key = ?)`,
		filter.Key, filter.Key).Scan(&page.Total).Error; err != nil {
		return page, fmt.Errorf("count settings history: %w", err)
	}

	var rows []settingHistoryRow
	if err := c.Raw(`
		SELECT h.uuid, h.setting_key, h.previous_value::text AS previous_value, h.new_value::text AS new_value,
			h.version, u.uuid AS changed_by, h.request_id, h.created_at
		FROM settings_history h LEFT JOIN users u ON u.id = h.changed_by
		WHERE (? = '' OR h.setting_key = ?)
		ORDER BY h.created_at DESC, h.id DESC
		LIMIT ? OFFSET ?`,
		filter.Key, filter.Key, filter.PerPage, (filter.Page-1)*filter.PerPage).Scan(&rows).Error; err != nil {
		return page, fmt.Errorf("list settings history: %w", err)
	}

	for _, row := range rows {
		entry := settingsdomain.HistoryEntry{
			UUID: row.UUID, Key: row.SettingKey, New: []byte(row.NewValue), Version: row.Version,
			ChangedBy: row.ChangedBy, RequestID: row.RequestID, CreatedAt: row.CreatedAt,
		}
		if row.PreviousValue != nil {
			entry.Previous = []byte(*row.PreviousValue)
		}

		page.Items = append(page.Items, entry)
	}

	return page, nil
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
