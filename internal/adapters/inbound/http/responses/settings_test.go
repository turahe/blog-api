package responses

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

var settingsStaffID = uuid.MustParse("0198a1b2-0000-7000-8000-000000007002")

func TestSetting(t *testing.T) {
	t.Parallel()

	definition := settingsdomain.Definition{
		Key:         "site.name",
		Category:    settingsdomain.CategorySite,
		Type:        settingsdomain.TypeString,
		Sensitivity: settingsdomain.PublicSafe,
		Description: "Site name",
		Default:     "Blog",
		MaxLength:   80,
	}

	tests := []struct {
		name    string
		setting settingsdomain.Setting
		want    string
	}{
		{
			name:    "coded default",
			setting: settingsdomain.Setting{Definition: definition, Value: "Blog", Defaulted: true},
			want: `{"key": "site.name", "value": "Blog", "valueType": "string", "category": "site",
				"sensitivity": "public_safe", "description": "Site name", "default": "Blog", "version": 0,
				"updatedAt": null, "updatedBy": null}`,
		},
		{
			name: "stored value",
			setting: settingsdomain.Setting{
				Definition: definition,
				Value:      "Gopher Blog",
				Version:    3,
				UpdatedAt:  new(time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60))),
				UpdatedBy:  &settingsStaffID,
			},
			want: `{"key": "site.name", "value": "Gopher Blog", "valueType": "string", "category": "site",
				"sensitivity": "public_safe", "description": "Site name", "default": "Blog", "version": 3,
				"updatedAt": "2026-08-01T10:00:00Z", "updatedBy": "0198a1b2-0000-7000-8000-000000007002"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Setting(tt.setting))
		})
	}
}

func TestSettingChange(t *testing.T) {
	t.Parallel()

	change := settingsdomain.Change{
		Key:         "posts.per_page",
		Previous:    int64(10),
		New:         int64(20),
		Version:     2,
		Sensitivity: settingsdomain.AdminOnly,
	}

	assertJSON(t, `{"key": "posts.per_page", "previousValue": 10, "newValue": 20, "version": 2}`, SettingChange(change))
}

func TestSettingHistory(t *testing.T) {
	t.Parallel()

	entry := settingsdomain.HistoryEntry{
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-000000007001"),
		Key:       "site.name",
		Previous:  json.RawMessage(`"Blog"`),
		New:       json.RawMessage(`"Gopher Blog"`),
		Version:   3,
		ChangedBy: &settingsStaffID,
		RequestID: "req-3",
		CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}

	redacted := entry
	redacted.Key = "mail.smtp_password"
	redacted.Previous, redacted.New, redacted.Redacted = nil, nil, true
	redacted.ChangedBy, redacted.RequestID = nil, ""

	tests := []struct {
		name  string
		entry settingsdomain.HistoryEntry
		want  string
	}{
		{
			name:  "visible values",
			entry: entry,
			want: `{"id": "0198a1b2-0000-7000-8000-000000007001", "key": "site.name", "previousValue": "Blog",
				"newValue": "Gopher Blog", "redacted": false, "version": 3,
				"changedBy": "0198a1b2-0000-7000-8000-000000007002", "requestId": "req-3",
				"createdAt": "2026-08-01T10:00:00Z"}`,
		},
		{
			name:  "redacted values are null",
			entry: redacted,
			want: `{"id": "0198a1b2-0000-7000-8000-000000007001", "key": "mail.smtp_password", "previousValue": null,
				"newValue": null, "redacted": true, "version": 3, "changedBy": null, "requestId": null,
				"createdAt": "2026-08-01T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, SettingHistory(tt.entry))
		})
	}
}
