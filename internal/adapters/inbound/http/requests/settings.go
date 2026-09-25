package requests

import "encoding/json"

// UpdateSettings is PUT /api/v1/admin/settings.
type UpdateSettings struct {
	Updates []SettingUpdate `json:"updates" binding:"required,min=1,max=100,dive"`
}

// SettingUpdate is one key. Version, when sent, must equal the key's current version.
type SettingUpdate struct {
	Key     string          `json:"key" binding:"required,max=128"`
	Value   json.RawMessage `json:"value" swaggertype:"object"`
	Version *int64          `json:"version" binding:"omitempty,min=0"`
}
