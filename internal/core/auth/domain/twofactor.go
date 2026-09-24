package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Two-factor errors; handlers map them via service.MapError.
var (
	ErrTwoFactorUnavailable    = errors.New("two-factor authentication is not configured")
	ErrTwoFactorNotEnrolled    = errors.New("two-factor authentication is not enabled")
	ErrTwoFactorAlreadyEnabled = errors.New("two-factor authentication is already enabled")
	ErrTwoFactorPending        = errors.New("two-factor setup has not been started")
	ErrTwoFactorInvalidCode    = errors.New("invalid two-factor code")
	ErrChallengeInvalid        = errors.New("two-factor challenge is invalid or expired")
)

// TwoFactor is a user's TOTP enrollment. SecretCiphertext is encrypted at rest;
// ConfirmedAt is nil until the user proves a working authenticator.
type TwoFactor struct {
	UserUUID             uuid.UUID
	SecretCiphertext     string
	ConfirmedAt          *time.Time
	LastUsedStep         int64
	BackupCodesRemaining int
}

// Enabled reports whether the enrollment has been confirmed.
func (t TwoFactor) Enabled() bool { return t.ConfirmedAt != nil }

// TwoFactorSetup is returned once when enrollment starts.
type TwoFactorSetup struct {
	Secret     string // base32, for manual entry
	OTPAuthURL string // otpauth:// URI for a QR code
}

// TwoFactorStatus describes a user's enrollment.
type TwoFactorStatus struct {
	Enabled              bool
	Pending              bool
	ConfirmedAt          *time.Time
	BackupCodesRemaining int
}

// PendingLogin is a password-verified login waiting for its second factor.
type PendingLogin struct {
	UserUUID  uuid.UUID
	Remember  bool
	UserAgent string
	IPAddress string
}

// TwoFactorChallenge is handed to the client instead of tokens when the
// account has two-factor authentication enabled.
type TwoFactorChallenge struct {
	Token     string
	ExpiresAt time.Time
}

// LoginResult is either a token pair or, for two-factor accounts, a challenge.
type LoginResult struct {
	Tokens    TokenPair
	Challenge *TwoFactorChallenge
}
