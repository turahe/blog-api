package requests

// PatchProfile documents PATCH /api/v1/me/profile and /api/v1/admin/users/:id/profile.
// Omitted keys are kept, null clears nullable fields, and unknown keys are rejected.
type PatchProfile struct {
	FullName         *string      `json:"full_name"`
	DisplayName      *string      `json:"display_name"`
	Bio              *string      `json:"bio"`
	ContactWebsite   *string      `json:"contact_website"`
	ContactLocation  *string      `json:"contact_location"`
	SocialLinks      *SocialLinks `json:"social_links"`
	Locale           *string      `json:"locale" example:"en_US"`
	Timezone         *string      `json:"timezone" example:"Asia/Jakarta"`
	MarketingConsent *bool        `json:"marketing_consent"`
}

// SocialLinks are handles (a leading @ is dropped); null or "" clears one.
type SocialLinks struct {
	Twitter  *string `json:"twitter"`
	LinkedIn *string `json:"linkedin"`
	GitHub   *string `json:"github"`
}

// RequestEmailChange is POST /api/v1/me/email/request-change.
type RequestEmailChange struct {
	NewEmail      string `json:"new_email" binding:"required,max=254"`
	PasswordProof string `json:"password_proof" binding:"required,max=128"`
}

// ConfirmEmailChange is POST /api/v1/me/email/confirm-change.
type ConfirmEmailChange struct {
	Token string `json:"token" binding:"required,max=512"`
}
