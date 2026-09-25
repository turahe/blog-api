package responses

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
)

// Setting serializes a resolved setting.
func Setting(s settingsdomain.Setting) gin.H {
	return gin.H{
		"key":         s.Key,
		"value":       s.Value,
		"value_type":  s.Type,
		"category":    s.Category,
		"sensitivity": s.Sensitivity,
		"description": s.Description,
		"default":     s.Default,
		"version":     s.Version,
		"updated_at":  RFC3339(s.UpdatedAt),
		"updated_by":  uuidString(s.UpdatedBy),
	}
}

// SettingChange serializes one applied key.
func SettingChange(c settingsdomain.Change) gin.H {
	return gin.H{
		"key":            c.Key,
		"previous_value": c.Previous,
		"new_value":      c.New,
		"version":        c.Version,
	}
}

// SettingHistory serializes one history row; values are null when redacted.
func SettingHistory(e settingsdomain.HistoryEntry) gin.H {
	return gin.H{
		"id":             e.UUID.String(),
		"key":            e.Key,
		"previous_value": rawOrNil(e.Previous),
		"new_value":      rawOrNil(e.New),
		"redacted":       e.Redacted,
		"version":        e.Version,
		"changed_by":     uuidString(e.ChangedBy),
		"request_id":     emptyToNil(e.RequestID),
		"created_at":     e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func rawOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}

	return raw
}
