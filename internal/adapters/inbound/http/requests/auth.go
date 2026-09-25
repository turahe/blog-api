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
	ChallengeToken string `json:"challengeToken" binding:"required,max=128"`
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

// OAuthCallback is POST /api/v1/auth/oauth/{provider}/callback.
type OAuthCallback struct {
	Code  string `json:"code" binding:"required,max=2048"`
	State string `json:"state" binding:"required,max=128"`
}

// Refresh is POST /api/v1/auth/refresh.
type Refresh struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// Logout is POST /api/v1/auth/logout (body optional).
type Logout struct {
	RefreshToken string `json:"refreshToken" binding:"omitempty"`
}

// Register is POST /api/v1/auth/register.
type Register struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Username string `json:"username" binding:"required,min=3,max=32"`
	FullName string `json:"fullName" binding:"required,max=120"`
	Password string `json:"password" binding:"required,min=12,max=128"`
}

// VerifyEmail is POST /api/v1/auth/verify-email. Password is the one chosen at sign-up.
type VerifyEmail struct {
	Token    string `json:"token" binding:"required,max=512"`
	Password string `json:"password" binding:"required,max=128"`
}

// ForgotPassword is POST /api/v1/auth/password/forgot.
type ForgotPassword struct {
	EmailOrUsername string `json:"emailOrUsername" binding:"required"`
}

// ResetPassword is POST /api/v1/auth/password/reset.
type ResetPassword struct {
	Token           string `json:"token" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required,min=12,max=128"`
	ConfirmPassword string `json:"confirmPassword" binding:"required,eqfield=NewPassword"`
}

// ChangePassword is PUT /api/v1/me/password.
type ChangePassword struct {
	CurrentPassword   string `json:"currentPassword" binding:"required"`
	NewPassword       string `json:"newPassword" binding:"required,min=12,max=128"`
	ConfirmPassword   string `json:"confirmPassword" binding:"required,eqfield=NewPassword"`
	RevokeAllSessions *bool  `json:"revokeAllSessions"`
}
