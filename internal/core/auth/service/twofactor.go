package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/core/audit"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/auth/totp"
)

const (
	challengeTTL         = 5 * time.Minute
	maxChallengeAttempts = 5
	backupCodeCount      = 10
	totpSkew             = 1
	backupCodeAlphabet   = "abcdefghjkmnpqrstuvwxyz23456789" // no look-alikes (0/o, 1/l/i)
)

type twoFactorDeps struct {
	repo       ports.TwoFactorRepository
	box        ports.SecretBox
	challenges ports.ChallengeStore
	issuer     string
}

// WithTwoFactor enables TOTP enrollment and the login challenge. box may be nil
// when no encryption key is configured: enrollment is then unavailable, and
// logins of already-enrolled accounts fail closed instead of skipping the factor.
func (s *AuthService) WithTwoFactor(repo ports.TwoFactorRepository, box ports.SecretBox, challenges ports.ChallengeStore, issuer string) *AuthService {
	s.mfa = twoFactorDeps{repo: repo, box: box, challenges: challenges, issuer: issuer}
	return s
}

// startTwoFactor returns a challenge when the user has two-factor enabled, or nil.
func (s *AuthService) startTwoFactor(ctx context.Context, userID uuid.UUID, login authdomain.PendingLogin) (*authdomain.TwoFactorChallenge, error) {
	if s.mfa.repo == nil {
		return nil, nil
	}

	enrollment, err := s.mfa.repo.Find(ctx, userID)
	if errors.Is(err, authdomain.ErrTwoFactorNotEnrolled) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("load two-factor enrollment: %w", err)
	}

	if !enrollment.Enabled() {
		return nil, nil
	}

	if s.mfa.box == nil || s.mfa.challenges == nil {
		return nil, authdomain.ErrTwoFactorUnavailable
	}

	raw, err := randomToken()
	if err != nil {
		return nil, err
	}

	if err := s.mfa.challenges.Save(ctx, hashChallenge(raw), login, challengeTTL); err != nil {
		return nil, fmt.Errorf("save two-factor challenge: %w", err)
	}

	return &authdomain.TwoFactorChallenge{Token: raw, ExpiresAt: s.clock.Now().Add(challengeTTL)}, nil
}

// CompleteTwoFactor exchanges a login challenge and a TOTP or backup code for a
// token pair. A challenge dies after maxChallengeAttempts wrong codes.
func (s *AuthService) CompleteTwoFactor(ctx context.Context, challengeToken, code string) (authdomain.TokenPair, error) {
	if err := s.twoFactorReady(); err != nil {
		return authdomain.TokenPair{}, err
	}

	hash := hashChallenge(strings.TrimSpace(challengeToken))

	login, err := s.mfa.challenges.Get(ctx, hash)
	if err != nil {
		return authdomain.TokenPair{}, err
	}

	audit.SetActor(ctx, login.UserUUID)

	// Count the attempt before checking the code so parallel guesses cannot
	// exceed the limit.
	attempts, err := s.mfa.challenges.Attempt(ctx, hash)
	if err != nil {
		return authdomain.TokenPair{}, fmt.Errorf("count two-factor attempt: %w", err)
	}

	if attempts > maxChallengeAttempts {
		_, _ = s.mfa.challenges.Consume(ctx, hash)
		return authdomain.TokenPair{}, authdomain.ErrChallengeInvalid
	}

	if err := s.verifyCode(ctx, login.UserUUID, code); err != nil {
		switch {
		case errors.Is(err, authdomain.ErrTwoFactorNotEnrolled):
			_, _ = s.mfa.challenges.Consume(ctx, hash)
			return authdomain.TokenPair{}, authdomain.ErrChallengeInvalid
		case errors.Is(err, authdomain.ErrTwoFactorInvalidCode) && attempts >= maxChallengeAttempts:
			_, _ = s.mfa.challenges.Consume(ctx, hash)
		}

		return authdomain.TokenPair{}, err
	}

	consumed, err := s.mfa.challenges.Consume(ctx, hash)
	if err != nil {
		return authdomain.TokenPair{}, fmt.Errorf("consume two-factor challenge: %w", err)
	}

	if !consumed {
		return authdomain.TokenPair{}, authdomain.ErrChallengeInvalid
	}

	user, err := s.activeUser(ctx, login.UserUUID)
	if err != nil {
		return authdomain.TokenPair{}, err
	}

	return s.completeLogin(ctx, user, login.UserAgent, login.IPAddress, login.Remember)
}

// TwoFactorStatus reports the user's enrollment.
func (s *AuthService) TwoFactorStatus(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactorStatus, error) {
	if s.mfa.repo == nil {
		return authdomain.TwoFactorStatus{}, authdomain.ErrTwoFactorUnavailable
	}

	enrollment, err := s.mfa.repo.Find(ctx, userID)
	if errors.Is(err, authdomain.ErrTwoFactorNotEnrolled) {
		return authdomain.TwoFactorStatus{}, nil
	}

	if err != nil {
		return authdomain.TwoFactorStatus{}, err
	}

	return authdomain.TwoFactorStatus{
		Enabled: enrollment.Enabled(), Pending: !enrollment.Enabled(),
		ConfirmedAt: enrollment.ConfirmedAt, BackupCodesRemaining: enrollment.BackupCodesRemaining,
	}, nil
}

// SetupTwoFactor starts (or restarts) enrollment with a fresh secret. It is
// refused once two-factor is enabled; disable it first to rotate the secret.
func (s *AuthService) SetupTwoFactor(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactorSetup, error) {
	if err := s.twoFactorReady(); err != nil {
		return authdomain.TwoFactorSetup{}, err
	}

	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return authdomain.TwoFactorSetup{}, err
	}

	enrollment, err := s.mfa.repo.Find(ctx, userID)
	if err == nil && enrollment.Enabled() {
		return authdomain.TwoFactorSetup{}, authdomain.ErrTwoFactorAlreadyEnabled
	}

	if err != nil && !errors.Is(err, authdomain.ErrTwoFactorNotEnrolled) {
		return authdomain.TwoFactorSetup{}, err
	}

	secret, err := totp.NewSecret()
	if err != nil {
		return authdomain.TwoFactorSetup{}, err
	}

	ciphertext, err := s.mfa.box.Encrypt(secret)
	if err != nil {
		return authdomain.TwoFactorSetup{}, fmt.Errorf("encrypt totp secret: %w", err)
	}

	if err := s.mfa.repo.SavePending(ctx, userID, ciphertext, s.clock.Now()); err != nil {
		return authdomain.TwoFactorSetup{}, err
	}

	return authdomain.TwoFactorSetup{
		Secret: totp.Encode(secret), OTPAuthURL: totp.URL(s.mfa.issuer, user.Email, secret),
	}, nil
}

// ConfirmTwoFactor enables a pending enrollment once the user proves a working
// authenticator, and returns the backup codes. They are shown only this once.
func (s *AuthService) ConfirmTwoFactor(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	if err := s.twoFactorReady(); err != nil {
		return nil, err
	}

	enrollment, err := s.mfa.repo.Find(ctx, userID)
	if errors.Is(err, authdomain.ErrTwoFactorNotEnrolled) {
		return nil, authdomain.ErrTwoFactorPending
	}

	if err != nil {
		return nil, err
	}

	if enrollment.Enabled() {
		return nil, authdomain.ErrTwoFactorAlreadyEnabled
	}

	step, err := s.checkTOTP(enrollment, code)
	if err != nil {
		return nil, err
	}

	codes, hashes, err := s.newBackupCodes()
	if err != nil {
		return nil, err
	}

	if err := s.mfa.repo.Confirm(ctx, userID, step, hashes, s.clock.Now()); err != nil {
		return nil, err
	}

	return codes, nil
}

// DisableTwoFactor removes the enrollment after re-checking the password and a
// current TOTP or backup code.
func (s *AuthService) DisableTwoFactor(ctx context.Context, userID uuid.UUID, password, code string) error {
	if err := s.twoFactorReady(); err != nil {
		return err
	}

	if err := s.checkPassword(ctx, userID, password); err != nil {
		return err
	}

	if err := s.verifyCode(ctx, userID, code); err != nil {
		return err
	}

	return s.mfa.repo.Delete(ctx, userID)
}

// RegenerateBackupCodes replaces every backup code after checking a current TOTP code.
func (s *AuthService) RegenerateBackupCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	if err := s.twoFactorReady(); err != nil {
		return nil, err
	}

	enrollment, err := s.enabledEnrollment(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := s.useTOTP(ctx, enrollment, code); err != nil {
		return nil, err
	}

	codes, hashes, err := s.newBackupCodes()
	if err != nil {
		return nil, err
	}

	if err := s.mfa.repo.ReplaceBackupCodes(ctx, userID, hashes, s.clock.Now()); err != nil {
		return nil, err
	}

	return codes, nil
}

func (s *AuthService) twoFactorReady() error {
	if s.mfa.repo == nil || s.mfa.box == nil || s.mfa.challenges == nil {
		return authdomain.ErrTwoFactorUnavailable
	}

	return nil
}

func (s *AuthService) enabledEnrollment(ctx context.Context, userID uuid.UUID) (authdomain.TwoFactor, error) {
	enrollment, err := s.mfa.repo.Find(ctx, userID)
	if err != nil {
		return authdomain.TwoFactor{}, err
	}

	if !enrollment.Enabled() {
		return authdomain.TwoFactor{}, authdomain.ErrTwoFactorNotEnrolled
	}

	return enrollment, nil
}

// verifyCode accepts a current TOTP code or an unused backup code for an enabled enrollment.
func (s *AuthService) verifyCode(ctx context.Context, userID uuid.UUID, code string) error {
	enrollment, err := s.enabledEnrollment(ctx, userID)
	if err != nil {
		return err
	}

	code = strings.TrimSpace(code)
	if isNumeric(code) {
		return s.useTOTP(ctx, enrollment, code)
	}

	normalized := normalizeBackupCode(code)
	if normalized == "" {
		return authdomain.ErrTwoFactorInvalidCode
	}

	used, err := s.mfa.repo.UseBackupCode(ctx, userID, s.mfa.box.MAC(normalized), s.clock.Now())
	if err != nil {
		return err
	}

	if !used {
		return authdomain.ErrTwoFactorInvalidCode
	}

	return nil
}

// useTOTP checks code and marks its time step used, so a code cannot be replayed.
func (s *AuthService) useTOTP(ctx context.Context, enrollment authdomain.TwoFactor, code string) error {
	step, err := s.checkTOTP(enrollment, code)
	if err != nil {
		return err
	}

	fresh, err := s.mfa.repo.UseStep(ctx, enrollment.UserUUID, step)
	if err != nil {
		return err
	}

	if !fresh {
		return authdomain.ErrTwoFactorInvalidCode
	}

	return nil
}

func (s *AuthService) checkTOTP(enrollment authdomain.TwoFactor, code string) (int64, error) {
	secret, err := s.mfa.box.Decrypt(enrollment.SecretCiphertext)
	if err != nil {
		return 0, fmt.Errorf("decrypt totp secret: %w", err)
	}

	step, ok := totp.Validate(secret, code, s.clock.Now(), totpSkew)
	if !ok || step <= enrollment.LastUsedStep {
		return 0, authdomain.ErrTwoFactorInvalidCode
	}

	return step, nil
}

func (s *AuthService) checkPassword(ctx context.Context, userID uuid.UUID, password string) error {
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return err
	}

	if user.PasswordHash == "" || !s.hasher.Compare(user.PasswordHash, password) {
		return authdomain.ErrCurrentPassword
	}

	return nil
}

func (s *AuthService) newBackupCodes() (codes, hashes []string, err error) {
	codes = make([]string, backupCodeCount)
	hashes = make([]string, backupCodeCount)

	for i := range codes {
		raw, err := randomString(backupCodeAlphabet, 10)
		if err != nil {
			return nil, nil, err
		}

		codes[i] = raw[:5] + "-" + raw[5:]
		hashes[i] = s.mfa.box.MAC(raw)
	}

	return codes, hashes, nil
}

func normalizeBackupCode(code string) string {
	code = strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(code) != 10 {
		return ""
	}

	for _, r := range code {
		if !strings.ContainsRune(backupCodeAlphabet, r) {
			return ""
		}
	}

	return code
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("challenge token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// randomString draws n uniform characters from alphabet, rejecting bytes past
// the largest multiple of len(alphabet) so no character is favored.
func randomString(alphabet string, n int) (string, error) {
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	buf := make([]byte, n)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("backup code: %w", err)
		}

		for _, b := range buf {
			if int(b) < limit && len(out) < n {
				out = append(out, alphabet[int(b)%len(alphabet)])
			}
		}
	}

	return string(out), nil
}

func hashChallenge(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
