// Package ports declares the interfaces the auth service depends on and exposes.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// UserRepository is the user storage the auth service needs.
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (userdomain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	FindByUsernameOrEmail(ctx context.Context, identity string) (userdomain.User, error)
	RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	Create(ctx context.Context, user userdomain.User) (userdomain.User, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string, changedAt time.Time) error
	// UpdateEmail sets a verified address; a live duplicate is authdomain.ErrEmailTaken.
	UpdateEmail(ctx context.Context, id uuid.UUID, email string, verifiedAt time.Time) error
}

// SessionRepository stores refresh sessions.
type SessionRepository interface {
	Create(ctx context.Context, session authdomain.RefreshSession) (authdomain.RefreshSession, error)
	FindByTokenHash(ctx context.Context, hash string) (authdomain.RefreshSession, error)
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error
	RevokeFamily(ctx context.Context, userID, familyID uuid.UUID, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error
	Replace(ctx context.Context, oldID, newID uuid.UUID, at time.Time) error
}

// ResetTokenRepository stores password reset tokens.
type ResetTokenRepository interface {
	Create(ctx context.Context, token authdomain.PasswordResetToken) error
	FindByHash(ctx context.Context, hash string) (authdomain.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error
	// RevokePending marks the user's unused tokens of purpose as used.
	RevokePending(ctx context.Context, userID uuid.UUID, purpose string, at time.Time) error
}

// EmailChangeNotifier delivers account emails. Implementations must not fail
// the request; delivery problems are theirs to log or retry.
type EmailChangeNotifier interface {
	// EmailChangeRequested sends the confirmation token to the new address and a notice to the current one.
	EmailChangeRequested(ctx context.Context, user userdomain.User, newEmail, rawToken string, expiresAt time.Time)
	// EmailChanged tells both addresses that the change completed.
	EmailChanged(ctx context.Context, user userdomain.User, oldEmail, newEmail string)
	// PasswordReset sends the reset token to the account address.
	PasswordReset(ctx context.Context, user userdomain.User, rawToken string, expiresAt time.Time)
	// PasswordChanged tells the account that its password was updated.
	PasswordChanged(ctx context.Context, user userdomain.User)
}

// RegistrationNotifier delivers sign-up emails, under the same contract as EmailChangeNotifier.
type RegistrationNotifier interface {
	// AccountVerify sends the verification token to the registration's address.
	AccountVerify(ctx context.Context, registration authdomain.Registration, rawToken string)
	// AccountExists tells an account that someone tried to register its address again.
	AccountExists(ctx context.Context, user userdomain.User)
}

// RegistrationRepository stores sign-ups waiting for email verification.
type RegistrationRepository interface {
	// Create stores the registration unless its address already has maxLive unexpired ones,
	// and reports whether it was stored.
	Create(ctx context.Context, registration authdomain.Registration, maxLive int) (bool, error)
	// FindByTokenHash returns authdomain.ErrRegistrationTokenInvalid when no registration matches.
	FindByTokenHash(ctx context.Context, hash string) (authdomain.Registration, error)
	// Consume deletes the registration with tokenHash and every other one for the same address;
	// false when it was already gone.
	Consume(ctx context.Context, tokenHash string) (bool, error)
}

// RegistrationPolicy reports whether public sign-up is open.
type RegistrationPolicy interface {
	RegistrationOpen(ctx context.Context) (bool, error)
}

// Registrar is the public sign-up use-case API consumed by HTTP handlers.
type Registrar interface {
	Register(ctx context.Context, in authdomain.SignUp) error
	VerifyEmail(ctx context.Context, rawToken, password, userAgent, ip string) (authdomain.TokenPair, error)
}

// EmailChanger is the email change use-case API consumed by HTTP handlers.
type EmailChanger interface {
	RequestEmailChange(ctx context.Context, userID uuid.UUID, newEmail, password string) (authdomain.EmailChangeRequest, error)
	ConfirmEmailChange(ctx context.Context, userID uuid.UUID, rawToken string) (userdomain.User, error)
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) bool
}

// TokenService issues and verifies access, refresh, and reset tokens.
type TokenService interface {
	IssueAccess(claims authdomain.AccessClaims) (string, error)
	ParseAccess(token string) (authdomain.AccessClaims, error)
	IssueRefresh() (raw, hash string, err error)
	HashRefresh(raw string) string
	IssueResetToken() (raw, hash, jti string, err error)
	HashResetToken(raw string) string
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// LoginAttempts tracks failed logins per account key and locks the key after
// too many failures. Implementations decide the thresholds.
type LoginAttempts interface {
	// Locked returns how long key stays locked; zero when it is not locked.
	Locked(ctx context.Context, key string) (time.Duration, error)
	// Fail records a failed attempt and returns the lock duration when this failure locked key.
	Fail(ctx context.Context, key string) (time.Duration, error)
	// Reset clears the failure count after a successful login.
	Reset(ctx context.Context, key string) error
}

// ResetTokenSink optionally captures newly issued reset tokens (tests / local tooling only).
type ResetTokenSink interface {
	Capture(rawToken string)
}

// TwoFactorRepository stores TOTP enrollments and backup codes.
type TwoFactorRepository interface {
	// Find returns authdomain.ErrTwoFactorNotEnrolled when the user has no enrollment.
	Find(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactor, error)
	// SavePending starts or restarts an unconfirmed enrollment, discarding backup codes.
	SavePending(ctx context.Context, userID uuid.UUID, secretCiphertext string, at time.Time) error
	// Confirm enables the enrollment, records step as used, and stores the backup code hashes.
	Confirm(ctx context.Context, userID uuid.UUID, step int64, codeHashes []string, at time.Time) error
	// UseStep records step as used; false when it (or a later step) was already used.
	UseStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error)
	// UseBackupCode spends an unused code; false when no unused code matches.
	UseBackupCode(ctx context.Context, userID uuid.UUID, codeHash string, at time.Time) (bool, error)
	// ReplaceBackupCodes discards every code and stores new hashes.
	ReplaceBackupCodes(ctx context.Context, userID uuid.UUID, codeHashes []string, at time.Time) error
	// Delete removes the enrollment and its backup codes.
	Delete(ctx context.Context, userID uuid.UUID) error
}

// SecretBox encrypts TOTP secrets at rest and hashes backup codes with a server key.
type SecretBox interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
	MAC(value string) string
}

// ChallengeStore holds pending two-factor logins keyed by the hash of their token.
type ChallengeStore interface {
	Save(ctx context.Context, tokenHash string, login authdomain.PendingLogin, ttl time.Duration) error
	// Get returns authdomain.ErrChallengeInvalid for an unknown or expired token.
	Get(ctx context.Context, tokenHash string) (authdomain.PendingLogin, error)
	// Attempt counts one code attempt and returns the attempts so far, this one included.
	Attempt(ctx context.Context, tokenHash string) (int, error)
	// Consume deletes the challenge; false when it was already gone.
	Consume(ctx context.Context, tokenHash string) (bool, error)
}

// OAuthProvider is one social login provider (authorization code flow with PKCE).
type OAuthProvider interface {
	AuthorizeURL(state, codeChallenge, redirectURI string) string
	// Exchange trades the code for the provider's view of the user;
	// authdomain.ErrOAuthExchange when the provider refuses the code.
	Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (authdomain.OAuthIdentity, error)
}

// OAuthStateStore keeps pending authorization requests; Consume is single use and
// returns authdomain.ErrOAuthStateInvalid for an unknown or expired state.
type OAuthStateStore interface {
	Save(ctx context.Context, state string, value authdomain.OAuthState, ttl time.Duration) error
	Consume(ctx context.Context, state string) (authdomain.OAuthState, error)
}

// OAuthIdentityRepository links provider identities to users.
type OAuthIdentityRepository interface {
	// FindUser returns the linked user or authdomain.ErrOAuthNoAccount.
	FindUser(ctx context.Context, provider, subject string) (uuid.UUID, error)
	// Link stores the identity; authdomain.ErrOAuthLinkConflict when the user already
	// has a different identity at the provider.
	Link(ctx context.Context, userID uuid.UUID, identity authdomain.OAuthIdentity, at time.Time) error
	// Touch records a sign-in with the identity.
	Touch(ctx context.Context, provider, subject string, at time.Time) error
}

// Service is the auth use-case API consumed by HTTP handlers.
type Service interface {
	// Login returns a challenge instead of tokens when the account has two-factor enabled.
	Login(ctx context.Context, email, password, userAgent, ip string, remember bool) (authdomain.LoginResult, error)
	Refresh(ctx context.Context, refreshToken, userAgent, ip string) (authdomain.TokenPair, error)
	Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error
	ParseAccessToken(token string) (authdomain.AccessClaims, error)
	ForgotPassword(ctx context.Context, emailOrUsername string) error
	CheckResetToken(ctx context.Context, rawToken string) (authdomain.ResetTokenValidity, error)
	ResetPassword(ctx context.Context, rawToken, newPassword, confirmPassword string) error
	ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (changedAt time.Time, sessionsInvalidated bool, err error)
}
