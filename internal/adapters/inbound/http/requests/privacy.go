package requests

// UpdatePrivacy is a partial privacy update; omitted flags are kept. current_password is
// required only when visibility_profile narrows (public → unlisted → private).
type UpdatePrivacy struct {
	VisibilityProfile   *string `json:"visibility_profile" binding:"omitempty,oneof=public unlisted private" example:"private"`
	VisibilityEmail     *bool   `json:"visibility_email"`
	VisibilityContact   *bool   `json:"visibility_contact"`
	SearchAllowIndexing *bool   `json:"search_allow_indexing"`
	CurrentPassword     string  `json:"current_password" binding:"max=128"`
}

// EraseAccount confirms an account erasure with the current password.
type EraseAccount struct {
	CurrentPassword string `json:"current_password" binding:"required,max=128"`
}
