package requests

// StartImpersonation starts acting as targetUserId. twoFactorCode (TOTP or backup code) is
// required when the caller has two-factor enabled.
type StartImpersonation struct {
	TargetUserID    string `json:"targetUserId" binding:"required,uuid" example:"0b8f5c1e-3c1a-4f5e-9d7a-2a1b3c4d5e6f"`
	Reason          string `json:"reason" binding:"required,min=10,max=255" example:"Ticket #4521: author cannot publish"`
	CurrentPassword string `json:"currentPassword" binding:"required,max=128"`
	TwoFactorCode   string `json:"twoFactorCode" binding:"max=32" example:"123456"`
}
