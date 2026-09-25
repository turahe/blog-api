package requests

// UpdatePrivacy is a partial privacy update; omitted flags are kept. currentPassword is
// required only when visibilityProfile narrows (public → unlisted → private).
type UpdatePrivacy struct {
	VisibilityProfile   *string `json:"visibilityProfile" binding:"omitempty,oneof=public unlisted private" example:"private"`
	VisibilityEmail     *bool   `json:"visibilityEmail"`
	VisibilityContact   *bool   `json:"visibilityContact"`
	SearchAllowIndexing *bool   `json:"searchAllowIndexing"`
	CurrentPassword     string  `json:"currentPassword" binding:"max=128"`
}

// EraseAccount confirms an account erasure with the current password.
type EraseAccount struct {
	CurrentPassword string `json:"currentPassword" binding:"required,max=128"`
}
