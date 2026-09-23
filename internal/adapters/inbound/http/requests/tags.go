package requests

// CreateTag is POST /api/v1/admin/tags.
type CreateTag struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
	Slug string `json:"slug" binding:"omitempty,min=1,max=100"`
}

// UpdateTag is PATCH /api/v1/admin/tags/:id.
type UpdateTag struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=100"`
	Slug *string `json:"slug" binding:"omitempty,min=1,max=100"`
}

// MergeTag is POST /api/v1/admin/tags/:id/merge.
type MergeTag struct {
	IntoID string `json:"into_id" binding:"required,uuid"`
}
