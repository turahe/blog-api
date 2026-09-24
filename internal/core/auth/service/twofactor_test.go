package service_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	"github.com/turahe/blog-api/internal/core/auth/totp"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// fakeBox is a reversible stand-in for the AES-GCM secret box.
type fakeBox struct{}

func (fakeBox) Encrypt(p []byte) (string, error) { return "enc:" + hex.EncodeToString(p), nil }
func (fakeBox) MAC(v string) string              { return "mac:" + v }

func (fakeBox) Decrypt(c string) ([]byte, error) {
	raw, ok := strings.CutPrefix(c, "enc:")
	if !ok {
		return nil, errors.New("bad ciphertext")
	}

	return hex.DecodeString(raw)
}

type twoFactorRecord struct {
	tf    authdomain.TwoFactor
	codes map[string]bool // hash -> used
}

type memTwoFactor struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*twoFactorRecord
}

func newMemTwoFactor() *memTwoFactor { return &memTwoFactor{byID: map[uuid.UUID]*twoFactorRecord{}} }

func (m *memTwoFactor) Find(_ context.Context, userID uuid.UUID) (authdomain.TwoFactor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.byID[userID]
	if !ok {
		return authdomain.TwoFactor{}, authdomain.ErrTwoFactorNotEnrolled
	}

	tf := rec.tf
	tf.BackupCodesRemaining = 0

	for _, used := range rec.codes {
		if !used {
			tf.BackupCodesRemaining++
		}
	}

	return tf, nil
}

func (m *memTwoFactor) SavePending(_ context.Context, userID uuid.UUID, ciphertext string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rec, ok := m.byID[userID]; ok && rec.tf.Enabled() {
		return authdomain.ErrTwoFactorAlreadyEnabled
	}

	m.byID[userID] = &twoFactorRecord{
		tf:    authdomain.TwoFactor{UserUUID: userID, SecretCiphertext: ciphertext},
		codes: map[string]bool{},
	}

	return nil
}

func (m *memTwoFactor) Confirm(_ context.Context, userID uuid.UUID, step int64, hashes []string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.byID[userID]
	if !ok || rec.tf.Enabled() || rec.tf.LastUsedStep >= step {
		return authdomain.ErrTwoFactorInvalidCode
	}

	rec.tf.ConfirmedAt = &at
	rec.tf.LastUsedStep = step

	for _, h := range hashes {
		rec.codes[h] = false
	}

	return nil
}

func (m *memTwoFactor) UseStep(_ context.Context, userID uuid.UUID, step int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.byID[userID]
	if !ok || rec.tf.LastUsedStep >= step {
		return false, nil
	}

	rec.tf.LastUsedStep = step

	return true, nil
}

func (m *memTwoFactor) UseBackupCode(_ context.Context, userID uuid.UUID, hash string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.byID[userID]
	if !ok {
		return false, nil
	}

	if used, exists := rec.codes[hash]; !exists || used {
		return false, nil
	}

	rec.codes[hash] = true

	return true, nil
}

func (m *memTwoFactor) ReplaceBackupCodes(_ context.Context, userID uuid.UUID, hashes []string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec := m.byID[userID]
	rec.codes = map[string]bool{}

	for _, h := range hashes {
		rec.codes[h] = false
	}

	return nil
}

func (m *memTwoFactor) Delete(_ context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.byID, userID)

	return nil
}

type memChallenges struct {
	mu       sync.Mutex
	logins   map[string]authdomain.PendingLogin
	attempts map[string]int
}

func newMemChallenges() *memChallenges {
	return &memChallenges{logins: map[string]authdomain.PendingLogin{}, attempts: map[string]int{}}
}

func (m *memChallenges) Save(_ context.Context, hash string, login authdomain.PendingLogin, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logins[hash] = login

	return nil
}

func (m *memChallenges) Get(_ context.Context, hash string) (authdomain.PendingLogin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	login, ok := m.logins[hash]
	if !ok {
		return authdomain.PendingLogin{}, authdomain.ErrChallengeInvalid
	}

	return login, nil
}

func (m *memChallenges) Attempt(_ context.Context, hash string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.logins[hash]; !ok {
		return 0, authdomain.ErrChallengeInvalid
	}

	m.attempts[hash]++

	return m.attempts[hash], nil
}

func (m *memChallenges) Consume(_ context.Context, hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.logins[hash]
	delete(m.logins, hash)

	return ok, nil
}

const tfEmail, tfPassword = "mfa@example.com", "Correct-Horse-9"

type twoFactorFixture struct {
	svc        *authservice.AuthService
	users      *memUsers
	repo       *memTwoFactor
	challenges *memChallenges
	clock      *movableClock
	userID     uuid.UUID
}

func newTwoFactorFixture(t *testing.T) *twoFactorFixture {
	t.Helper()

	user := userdomain.User{
		UUID: uuid.New(), Email: tfEmail, Username: "mfa", FullName: "MFA",
		PasswordHash: "hash:" + tfPassword, Status: userdomain.StatusActive,
	}
	users, sessions, resets := newMemStores(user)
	// Start mid-step so advancing by one period always lands on the next step.
	clock := &movableClock{t: time.Unix(1_800_000_015, 0).UTC()}
	f := &twoFactorFixture{
		users: users, repo: newMemTwoFactor(), challenges: newMemChallenges(), clock: clock, userID: user.UUID,
	}
	f.svc = authservice.New(users, sessions, resets, fakeHasher{}, fakeTokens{}, clock, uuidGen{}, authservice.Config{}, nil).
		WithTwoFactor(f.repo, fakeBox{}, f.challenges, "Blog")

	return f
}

// code returns the current TOTP code for the fixture user.
func (f *twoFactorFixture) code(t *testing.T) string {
	t.Helper()

	tf, err := f.repo.Find(t.Context(), f.userID)
	require.NoError(t, err)

	secret, err := fakeBox{}.Decrypt(tf.SecretCiphertext)
	require.NoError(t, err)

	return totp.Code(secret, totp.Step(f.clock.Now()))
}

// enroll runs setup and confirm, advancing past the confirm step, and returns the backup codes.
func (f *twoFactorFixture) enroll(t *testing.T) []string {
	t.Helper()

	setup, err := f.svc.SetupTwoFactor(t.Context(), f.userID)
	require.NoError(t, err)
	require.NotEmpty(t, setup.Secret)
	require.True(t, strings.HasPrefix(setup.OTPAuthURL, "otpauth://totp/"))

	codes, err := f.svc.ConfirmTwoFactor(t.Context(), f.userID, f.code(t))
	require.NoError(t, err)
	f.clock.t = f.clock.t.Add(30 * time.Second)

	return codes
}

func (f *twoFactorFixture) challenge(t *testing.T) string {
	t.Helper()

	res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)
	require.NotNil(t, res.Challenge, "enrolled login must return a challenge")
	require.Empty(t, res.Tokens.AccessToken, "no tokens before the second factor")

	return res.Challenge.Token
}

func TestTwoFactorEnrollAndCompleteLogin(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	codes := f.enroll(t)
	require.Len(t, codes, 10)

	for _, c := range codes {
		require.Regexp(t, `^[a-z2-9]{5}-[a-z2-9]{5}$`, c)
	}

	status, err := f.svc.TwoFactorStatus(t.Context(), f.userID)
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.Equal(t, 10, status.BackupCodesRemaining)

	token := f.challenge(t)
	code := f.code(t)

	pair, err := f.svc.CompleteTwoFactor(t.Context(), token, code)
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.NotNil(t, f.users.byID[f.userID].LastLoginAt)

	_, err = f.svc.CompleteTwoFactor(t.Context(), token, code)
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid, "a challenge is single use")

	_, err = f.svc.CompleteTwoFactor(t.Context(), f.challenge(t), code)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode, "a TOTP code is single use")
}

func TestTwoFactorBackupCodeIsSingleUse(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	backup := f.enroll(t)[0]

	_, err := f.svc.CompleteTwoFactor(t.Context(), f.challenge(t), " "+strings.ToUpper(backup)+" ")
	require.NoError(t, err)

	_, err = f.svc.CompleteTwoFactor(t.Context(), f.challenge(t), backup)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode)

	status, err := f.svc.TwoFactorStatus(t.Context(), f.userID)
	require.NoError(t, err)
	require.Equal(t, 9, status.BackupCodesRemaining)
}

func TestTwoFactorChallengeDiesAfterAttemptLimit(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)
	token := f.challenge(t)

	for range 5 {
		_, err := f.svc.CompleteTwoFactor(t.Context(), token, "000000")
		require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode)
	}

	_, err := f.svc.CompleteTwoFactor(t.Context(), token, f.code(t))
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)
}

func TestTwoFactorFailsClosedWithoutKey(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)

	users, sessions, resets := newMemStores(f.users.byID[f.userID])
	keyless := authservice.New(users, sessions, resets, fakeHasher{}, fakeTokens{}, f.clock, uuidGen{}, authservice.Config{}, nil).
		WithTwoFactor(f.repo, nil, f.challenges, "Blog")

	_, err := keyless.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorUnavailable, "an enrolled account must not skip its second factor")

	_, err = keyless.SetupTwoFactor(t.Context(), uuid.New())
	require.ErrorIs(t, err, authdomain.ErrTwoFactorUnavailable)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 503, status)
}

func TestTwoFactorSetupAndConfirmGuards(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	_, err := f.svc.ConfirmTwoFactor(t.Context(), f.userID, "123456")
	require.ErrorIs(t, err, authdomain.ErrTwoFactorPending)

	_, err = f.svc.SetupTwoFactor(t.Context(), f.userID)
	require.NoError(t, err)

	_, err = f.svc.ConfirmTwoFactor(t.Context(), f.userID, "abc")
	require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode)

	res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)
	require.Nil(t, res.Challenge, "an unconfirmed enrollment does not gate login")

	_, err = f.svc.ConfirmTwoFactor(t.Context(), f.userID, f.code(t))
	require.NoError(t, err)

	_, err = f.svc.SetupTwoFactor(t.Context(), f.userID)
	require.ErrorIs(t, err, authdomain.ErrTwoFactorAlreadyEnabled)
}

func TestDisableTwoFactorNeedsPasswordAndCode(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	backup := f.enroll(t)

	err := f.svc.DisableTwoFactor(t.Context(), f.userID, "wrong", f.code(t))
	require.ErrorIs(t, err, authdomain.ErrCurrentPassword)

	err = f.svc.DisableTwoFactor(t.Context(), f.userID, tfPassword, "000000")
	require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode)

	require.NoError(t, f.svc.DisableTwoFactor(t.Context(), f.userID, tfPassword, backup[3]))

	res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
	require.NoError(t, err)
	require.Nil(t, res.Challenge)
	require.NotEmpty(t, res.Tokens.AccessToken)
}

func TestTwoFactorPendingChallengeDiesWhenDisabled(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)
	token := f.challenge(t)

	require.NoError(t, f.repo.Delete(t.Context(), f.userID))

	_, err := f.svc.CompleteTwoFactor(t.Context(), token, "123456")
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)
}

func TestRegenerateBackupCodesReplacesOldOnes(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	old := f.enroll(t)

	fresh, err := f.svc.RegenerateBackupCodes(t.Context(), f.userID, f.code(t))
	require.NoError(t, err)
	require.Len(t, fresh, 10)
	f.clock.t = f.clock.t.Add(30 * time.Second)

	_, err = f.svc.CompleteTwoFactor(t.Context(), f.challenge(t), old[0])
	require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode)

	_, err = f.svc.CompleteTwoFactor(t.Context(), f.challenge(t), fresh[0])
	require.NoError(t, err)
}
