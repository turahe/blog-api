// Package requests declares HTTP request bodies and binding/validation helpers.
package requests

// Login is POST /api/v1/auth/login.
type Login struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=1"`
	Remember bool   `json:"remember"`
}

// TwoFactorChallenge is POST /api/v1/auth/2fa/challenge. Code is a 6-digit TOTP
// code or a backup code.
type TwoFactorChallenge struct {
	ChallengeToken string `json:"challenge_token" binding:"required,max=128"`
	Code           string `json:"code" binding:"required,max=32"`
}

// TwoFactorCode is POST /api/v1/me/2fa/confirm and /me/2fa/backup-codes.
type TwoFactorCode struct {
	Code string `json:"code" binding:"required,max=32"`
}

// DisableTwoFactor is DELETE /api/v1/me/2fa.
type DisableTwoFactor struct {
	Password string `json:"password" binding:"required,max=128"`
	Code     string `json:"code" binding:"required,max=32"`
}

// Refresh is POST /api/v1/auth/refresh.
type Refresh struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Logout is POST /api/v1/auth/logout (body optional).
type Logout struct {
	RefreshToken string `json:"refresh_token" binding:"omitempty"`
}

// ForgotPassword is POST /api/v1/auth/password/forgot.
type ForgotPassword struct {
	EmailOrUsername string `json:"email_or_username" binding:"required"`
}

// ResetPassword is POST /api/v1/auth/password/reset.
type ResetPassword struct {
	Token           string `json:"token" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=12,max=128"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
}

// ChangePassword is PUT /api/v1/me/password.
type ChangePassword struct {
	CurrentPassword   string `json:"current_password" binding:"required"`
	NewPassword       string `json:"new_password" binding:"required,min=12,max=128"`
	ConfirmPassword   string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
	RevokeAllSessions *bool  `json:"revoke_all_sessions"`
}
