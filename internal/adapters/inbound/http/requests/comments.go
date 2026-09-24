package requests

// CreateComment is POST /api/v1/posts/:id/comments.
type CreateComment struct {
	// Plain text or markdown; HTML is not rendered.
	Content  string `json:"content" binding:"required,max=10000"`
	ParentID string `json:"parent_id" binding:"omitempty,uuid" format:"uuid"`
	// Guest commenters only; ignored when a bearer token is sent.
	AuthorName  string `json:"author_name" binding:"omitempty,max=100"`
	AuthorEmail string `json:"author_email" binding:"omitempty,email,max=254"`
	// Leave empty. Anti-spam trap for bots; must stay hidden in forms.
	Honeypot string `json:"honeypot" binding:"omitempty,max=500"`
}

// UpdateComment is PATCH /api/v1/comments/:id.
type UpdateComment struct {
	Content string `json:"content" binding:"required,max=10000"`
}

// FlagComment is POST /api/v1/comments/:id/flag.
type FlagComment struct {
	ReasonCode string `json:"reason_code" binding:"required,oneof=spam abuse hate harassment doxx self_harm copyright impersonation illegal other"`
	Details    string `json:"details" binding:"omitempty,max=2000"`
}
