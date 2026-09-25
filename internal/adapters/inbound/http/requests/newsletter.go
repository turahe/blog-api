package requests

import "time"

// NewsletterSubscribe asks for a double opt-in confirmation email. lists are list slugs; empty
// means the default lists. honeypot is a hidden form field that people leave empty.
type NewsletterSubscribe struct {
	Email             string   `json:"email" binding:"required,max=254" example:"reader@example.com"`
	DisplayName       string   `json:"display_name" binding:"max=100" example:"Alex"`
	Lists             []string `json:"lists" binding:"omitempty,max=20,dive,required,max=64" example:"weekly"`
	Format            string   `json:"format" binding:"omitempty,oneof=html plaintext" example:"html"`
	Honeypot          string   `json:"honeypot" binding:"max=200"`
	TurnstileResponse string   `json:"turnstile_response" binding:"max=2048"`
}

// NewsletterToken carries a raw token from a newsletter email link.
type NewsletterToken struct {
	Token string `json:"token" binding:"required,max=128"`
}

// NewsletterResend asks for a new confirmation email.
type NewsletterResend struct {
	Email string `json:"email" binding:"required,max=254" example:"reader@example.com"`
}

// NewsletterUnsubscribe stops all newsletter mail. The token may instead be sent as the token
// query parameter, as in the List-Unsubscribe one-click URL.
type NewsletterUnsubscribe struct {
	Token      string `json:"token" binding:"max=128"`
	ReasonCode string `json:"reason_code" binding:"omitempty,oneof=too_frequent not_relevant never_signed_up other"`
	Feedback   string `json:"feedback" binding:"max=1000"`
}

// NewsletterPreferences changes a subscription. Omitted fields stay unchanged; lists is the
// complete set of list slugs to receive, and an empty array unsubscribes.
type NewsletterPreferences struct {
	Format         *string   `json:"format" binding:"omitempty,oneof=html plaintext" example:"plaintext"`
	Lists          *[]string `json:"lists" binding:"omitempty,max=20,dive,required,max=64"`
	UnsubscribeAll bool      `json:"unsubscribe_all"`
}

// NewsletterMeSubscribe subscribes the account email; empty lists means the default lists.
type NewsletterMeSubscribe struct {
	Lists  []string `json:"lists" binding:"omitempty,max=20,dive,required,max=64" example:"weekly"`
	Format string   `json:"format" binding:"omitempty,oneof=html plaintext" example:"html"`
}

// NewsletterMeUnsubscribe leaves lists; empty lists leaves every list.
type NewsletterMeUnsubscribe struct {
	Lists      []string `json:"lists" binding:"omitempty,max=20,dive,required,max=64"`
	ReasonCode string   `json:"reason_code" binding:"omitempty,oneof=too_frequent not_relevant never_signed_up other"`
	Feedback   string   `json:"feedback" binding:"max=1000"`
}

// NewsletterIssueCreate creates an issue: draft (default), scheduled (needs send_at), or
// queued (send now). body_markdown is rendered to sanitized HTML and plain text.
type NewsletterIssueCreate struct {
	Subject      string     `json:"subject" binding:"required,max=200" example:"September highlights"`
	Preheader    string     `json:"preheader" binding:"max=200" example:"Three posts you may have missed"`
	BodyMarkdown string     `json:"body_markdown" binding:"required,max=200000" example:"## Hello\n\nThis month..."`
	Lists        []string   `json:"lists" binding:"required,min=1,max=20,dive,required,max=64" example:"weekly"`
	Status       string     `json:"status" binding:"omitempty,oneof=draft scheduled queued" example:"draft"`
	SendAt       *time.Time `json:"send_at" example:"2026-10-01T09:00:00Z"`
}

// NewsletterIssuePatch edits an issue. Omitted fields stay unchanged. Content and lists change
// only while the issue is draft or scheduled.
type NewsletterIssuePatch struct {
	Subject      *string    `json:"subject" binding:"omitempty,max=200"`
	Preheader    *string    `json:"preheader" binding:"omitempty,max=200"`
	BodyMarkdown *string    `json:"body_markdown" binding:"omitempty,max=200000"`
	Lists        *[]string  `json:"lists" binding:"omitempty,min=1,max=20,dive,required,max=64"`
	Status       *string    `json:"status" binding:"omitempty,oneof=draft scheduled queued cancelled" example:"cancelled"`
	SendAt       *time.Time `json:"send_at"`
}

// NewsletterProviderConfig replaces the stored sending settings and the list set. Lists
// missing from lists are archived.
type NewsletterProviderConfig struct {
	FromName            string           `json:"from_name" binding:"max=100" example:"Turahe Blog"`
	FromEmail           string           `json:"from_email" binding:"omitempty,max=254" example:"newsletter@example.com"`
	ReplyTo             string           `json:"reply_to" binding:"omitempty,max=254" example:"hello@example.com"`
	PostalAddress       string           `json:"postal_address" binding:"max=500" example:"123 Example Street, Jakarta, ID"`
	ConfirmTTLHours     int              `json:"confirm_ttl_hours" binding:"required,min=1,max=168" example:"48"`
	DoubleOptInRequired *bool            `json:"double_optin_required" binding:"required" example:"true"`
	Lists               []NewsletterList `json:"lists" binding:"required,min=1,max=20,dive"`
}

// NewsletterList is one list in a provider-config update.
type NewsletterList struct {
	Slug        string `json:"slug" binding:"required,max=64" example:"weekly"`
	Name        string `json:"name" binding:"required,max=100" example:"Weekly digest"`
	Description string `json:"description" binding:"max=500" example:"The week's posts, every Friday"`
	IsDefault   bool   `json:"is_default" example:"true"`
}
