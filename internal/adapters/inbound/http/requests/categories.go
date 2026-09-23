package requests

// CreateCategory is POST /api/v1/admin/categories.
type CreateCategory struct {
	Name        string  `json:"name" binding:"required,min=1,max=255"`
	Slug        string  `json:"slug" binding:"omitempty,min=1,max=255"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
	ParentID    *string `json:"parent_id" binding:"omitempty,uuid"`
	ImageID     *string `json:"image_id" binding:"omitempty,uuid"`
	BeforeID    *string `json:"before_id" binding:"omitempty,uuid"`
}
