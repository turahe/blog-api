package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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
	fail faults
}

func newMemTwoFactor() *memTwoFactor { return &memTwoFactor{byID: map[uuid.UUID]*twoFactorRecord{}} }

func (m *memTwoFactor) Find(_ context.Context, userID uuid.UUID) (authdomain.TwoFactor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.fail["Find"]; err != nil {
		return authdomain.TwoFactor{}, err
	}

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

	if err := m.fail["SavePending"]; err != nil {
		return err
	}

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

	if err := m.fail["Confirm"]; err != nil {
		return err
	}

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

	if err := m.fail["UseStep"]; err != nil {
		return false, err
	}

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

	if err := m.fail["UseBackupCode"]; err != nil {
		return false, err
	}

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

	if err := m.fail["ReplaceBackupCodes"]; err != nil {
		return err
	}

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

	if err := m.fail["Delete"]; err != nil {
		return err
	}

	delete(m.byID, userID)

	return nil
}

type memChallenges struct {
	mu       sync.Mutex
	logins   map[string]authdomain.PendingLogin
	attempts map[string]int
	fail     faults
}

func newMemChallenges() *memChallenges {
	return &memChallenges{logins: map[string]authdomain.PendingLogin{}, attempts: map[string]int{}}
}

func (m *memChallenges) Save(_ context.Context, hash string, login authdomain.PendingLogin, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.fail["Save"]; err != nil {
		return err
	}

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

	if err := m.fail["Attempt"]; err != nil {
		return 0, err
	}

	if _, ok := m.logins[hash]; !ok {
		return 0, authdomain.ErrChallengeInvalid
	}

	m.attempts[hash]++

	return m.attempts[hash], nil
}

func (m *memChallenges) Consume(_ context.Context, hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.fail["Consume"]; err != nil {
		return false, err
	}

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

// failingBox fails the SecretBox methods named in fail.
type failingBox struct {
	fakeBox

	fail faults
}

func (b failingBox) Encrypt(p []byte) (string, error) {
	if err := b.fail["Encrypt"]; err != nil {
		return "", err
	}

	return b.fakeBox.Encrypt(p)
}

func (b failingBox) Decrypt(c string) ([]byte, error) {
	if err := b.fail["Decrypt"]; err != nil {
		return nil, err
	}

	return b.fakeBox.Decrypt(c)
}

// racingChallenges loses every Consume to a concurrent request.
type racingChallenges struct{ *memChallenges }

func (r racingChallenges) Consume(ctx context.Context, hash string) (bool, error) {
	_, _ = r.memChallenges.Consume(ctx, hash)
	return false, nil
}

// replayedSteps reports every TOTP step as already used by a concurrent request.
type replayedSteps struct{ *memTwoFactor }

func (replayedSteps) UseStep(context.Context, uuid.UUID, int64) (bool, error) { return false, nil }

func challengeHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestLoginPropagatesTwoFactorFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(f *twoFactorFixture)
	}{
		{name: "enrollment lookup", setup: func(f *twoFactorFixture) { f.repo.fail = faults{"Find": errBoom} }},
		{name: "challenge store", setup: func(f *twoFactorFixture) { f.challenges.fail = faults{"Save": errBoom} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			f.enroll(t)
			tt.setup(f)

			res, err := f.svc.Login(t.Context(), tfEmail, tfPassword, "ua", "127.0.0.1", false)
			require.ErrorIs(t, err, errBoom)
			assert.Nil(t, res.Challenge)
			assert.Empty(t, res.Tokens.AccessToken, "a failed second-factor check never falls through to tokens")
		})
	}
}

func TestTwoFactorUnavailableWithoutConfiguration(t *testing.T) {
	t.Parallel()

	user := activeTestUser()
	users, sessions, resets := newMemStores(user)
	svc := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil)
	ctx := t.Context()

	calls := map[string]func() error{
		"complete": func() error { _, err := svc.CompleteTwoFactor(ctx, "token", "123456"); return err },
		"status":   func() error { _, err := svc.TwoFactorStatus(ctx, user.UUID); return err },
		"setup":    func() error { _, err := svc.SetupTwoFactor(ctx, user.UUID); return err },
		"confirm":  func() error { _, err := svc.ConfirmTwoFactor(ctx, user.UUID, "123456"); return err },
		"disable":  func() error { return svc.DisableTwoFactor(ctx, user.UUID, oldPassword, "123456") },
		"regenerate": func() error {
			_, err := svc.RegenerateBackupCodes(ctx, user.UUID, "123456")
			return err
		},
	}

	for name, call := range calls {
		require.ErrorIs(t, call(), authdomain.ErrTwoFactorUnavailable, name)
	}
}

func TestCompleteTwoFactorFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *twoFactorFixture, token string)
		code    func(t *testing.T, f *twoFactorFixture, backup []string) string
		wantErr error
	}{
		{
			name:    "attempt counter failure",
			setup:   func(_ *testing.T, f *twoFactorFixture, _ string) { f.challenges.fail = faults{"Attempt": errBoom} },
			wantErr: errBoom,
		},
		{
			name:    "backup code lookup failure",
			setup:   func(_ *testing.T, f *twoFactorFixture, _ string) { f.repo.fail = faults{"UseBackupCode": errBoom} },
			code:    func(_ *testing.T, _ *twoFactorFixture, backup []string) string { return backup[0] },
			wantErr: errBoom,
		},
		{
			name:    "consume failure",
			setup:   func(_ *testing.T, f *twoFactorFixture, _ string) { f.challenges.fail = faults{"Consume": errBoom} },
			wantErr: errBoom,
		},
		{
			name: "challenge consumed by a concurrent request",
			setup: func(_ *testing.T, f *twoFactorFixture, _ string) {
				f.svc.WithTwoFactor(f.repo, fakeBox{}, racingChallenges{f.challenges}, "Blog")
			},
			wantErr: authdomain.ErrChallengeInvalid,
		},
		{
			name: "account suspended during the challenge",
			setup: func(_ *testing.T, f *twoFactorFixture, _ string) {
				u := f.users.byID[f.userID]
				u.Status = userdomain.StatusSuspended
				f.users.byID[f.userID] = u
			},
			wantErr: authdomain.ErrUserInactive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			backup := f.enroll(t)
			token := f.challenge(t)
			tt.setup(t, f, token)

			code := f.code(t)
			if tt.code != nil {
				code = tt.code(t, f, backup)
			}

			pair, err := f.svc.CompleteTwoFactor(t.Context(), token, code)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, pair.AccessToken)
			assert.Nil(t, f.users.byID[f.userID].LastLoginAt, "no login is recorded")
		})
	}
}

func TestCompleteTwoFactorBurnsChallengeOverAttemptLimit(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)
	token := f.challenge(t)

	f.challenges.mu.Lock()
	f.challenges.attempts[challengeHash(token)] = 5
	f.challenges.mu.Unlock()

	_, err := f.svc.CompleteTwoFactor(t.Context(), token, f.code(t))
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid)

	_, err = f.challenges.Get(t.Context(), challengeHash(token))
	require.ErrorIs(t, err, authdomain.ErrChallengeInvalid, "the challenge is consumed")
}

func TestTwoFactorStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *twoFactorFixture)
		want    authdomain.TwoFactorStatus
		wantErr error
	}{
		{name: "not enrolled", setup: func(*testing.T, *twoFactorFixture) {}, want: authdomain.TwoFactorStatus{}},
		{
			name: "pending",
			setup: func(t *testing.T, f *twoFactorFixture) {
				_, err := f.svc.SetupTwoFactor(t.Context(), f.userID)
				require.NoError(t, err)
			},
			want: authdomain.TwoFactorStatus{Pending: true},
		},
		{
			name:    "lookup failure",
			setup:   func(_ *testing.T, f *twoFactorFixture) { f.repo.fail = faults{"Find": errBoom} },
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			tt.setup(t, f)

			status, err := f.svc.TwoFactorStatus(t.Context(), f.userID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, status)
		})
	}
}

func TestSetupTwoFactorFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		userID  func(f *twoFactorFixture) uuid.UUID
		setup   func(f *twoFactorFixture)
		wantErr error
	}{
		{
			name: "unknown account", userID: func(*twoFactorFixture) uuid.UUID { return uuid.New() },
			setup: func(*twoFactorFixture) {}, wantErr: authdomain.ErrUserInactive,
		},
		{name: "enrollment lookup", setup: func(f *twoFactorFixture) { f.repo.fail = faults{"Find": errBoom} }, wantErr: errBoom},
		{
			name: "encrypt secret",
			setup: func(f *twoFactorFixture) {
				f.svc.WithTwoFactor(f.repo, failingBox{fail: faults{"Encrypt": errBoom}}, f.challenges, "Blog")
			},
			wantErr: errBoom,
		},
		{name: "save pending", setup: func(f *twoFactorFixture) { f.repo.fail = faults{"SavePending": errBoom} }, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			tt.setup(f)

			userID := f.userID
			if tt.userID != nil {
				userID = tt.userID(f)
			}

			setup, err := f.svc.SetupTwoFactor(t.Context(), userID)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, setup.Secret)
		})
	}
}

func TestConfirmTwoFactorFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *twoFactorFixture)
		wantErr error
	}{
		{
			name:    "enrollment lookup",
			setup:   func(_ *testing.T, f *twoFactorFixture) { f.repo.fail = faults{"Find": errBoom} },
			wantErr: errBoom,
		},
		{
			name:    "already enabled",
			setup:   func(t *testing.T, f *twoFactorFixture) { f.enroll(t) },
			wantErr: authdomain.ErrTwoFactorAlreadyEnabled,
		},
		{
			name: "store confirmation",
			setup: func(t *testing.T, f *twoFactorFixture) {
				_, err := f.svc.SetupTwoFactor(t.Context(), f.userID)
				require.NoError(t, err)

				f.repo.fail = faults{"Confirm": errBoom}
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			tt.setup(t, f)

			code := "123456"
			if tf, err := f.repo.Find(t.Context(), f.userID); err == nil && tf.SecretCiphertext != "" {
				code = f.code(t)
			}

			codes, err := f.svc.ConfirmTwoFactor(t.Context(), f.userID, code)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, codes)
		})
	}
}

func TestRegenerateBackupCodesFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *twoFactorFixture)
		code    string
		wantErr error
	}{
		{name: "not enrolled", setup: func(*testing.T, *twoFactorFixture) {}, wantErr: authdomain.ErrTwoFactorNotEnrolled},
		{
			name: "enrollment not confirmed",
			setup: func(t *testing.T, f *twoFactorFixture) {
				_, err := f.svc.SetupTwoFactor(t.Context(), f.userID)
				require.NoError(t, err)
			},
			wantErr: authdomain.ErrTwoFactorNotEnrolled,
		},
		{
			name: "wrong code", setup: func(t *testing.T, f *twoFactorFixture) { f.enroll(t) },
			code: "000000", wantErr: authdomain.ErrTwoFactorInvalidCode,
		},
		{
			name: "store codes",
			setup: func(t *testing.T, f *twoFactorFixture) {
				f.enroll(t)
				f.repo.fail = faults{"ReplaceBackupCodes": errBoom}
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			tt.setup(t, f)

			code := tt.code
			if code == "" {
				code = "123456"
				if tf, err := f.repo.Find(t.Context(), f.userID); err == nil && tf.SecretCiphertext != "" {
					code = f.code(t)
				}
			}

			codes, err := f.svc.RegenerateBackupCodes(t.Context(), f.userID, code)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, codes)
		})
	}
}

func TestDisableTwoFactorRejectsMalformedCodes(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	f.enroll(t)

	for _, code := range []string{"", "   ", "abc", "abcde-fghi1", "0000 00", "abcdefghjkm"} {
		err := f.svc.DisableTwoFactor(t.Context(), f.userID, tfPassword, code)
		require.ErrorIs(t, err, authdomain.ErrTwoFactorInvalidCode, "code %q", code)
	}

	status, err := f.svc.TwoFactorStatus(t.Context(), f.userID)
	require.NoError(t, err)
	assert.True(t, status.Enabled, "two-factor stays on")
}

func TestTOTPVerificationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(f *twoFactorFixture)
		wantErr error
	}{
		{name: "step store failure", setup: func(f *twoFactorFixture) { f.repo.fail = faults{"UseStep": errBoom} }, wantErr: errBoom},
		{
			name: "step used by a concurrent request",
			setup: func(f *twoFactorFixture) {
				f.svc.WithTwoFactor(replayedSteps{f.repo}, fakeBox{}, f.challenges, "Blog")
			},
			wantErr: authdomain.ErrTwoFactorInvalidCode,
		},
		{
			name: "secret cannot be decrypted",
			setup: func(f *twoFactorFixture) {
				f.svc.WithTwoFactor(f.repo, failingBox{fail: faults{"Decrypt": errBoom}}, f.challenges, "Blog")
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			f.enroll(t)
			code := f.code(t)
			tt.setup(f)

			err := f.svc.DisableTwoFactor(t.Context(), f.userID, tfPassword, code)
			require.ErrorIs(t, err, tt.wantErr)

			_, findErr := f.repo.Find(t.Context(), f.userID)
			require.NoError(t, findErr, "the enrollment is kept")
		})
	}
}
