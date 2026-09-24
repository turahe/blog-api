package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

type fakeTwoFactor struct {
	token, code, password string
	user                  uuid.UUID
	err                   error
}

func (f *fakeTwoFactor) CompleteTwoFactor(_ context.Context, token, code string) (authdomain.TokenPair, error) {
	f.token, f.code = token, code
	return authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 900}, f.err
}

func (f *fakeTwoFactor) TwoFactorStatus(_ context.Context, id uuid.UUID) (authdomain.TwoFactorStatus, error) {
	f.user = id
	return authdomain.TwoFactorStatus{Enabled: true, BackupCodesRemaining: 7}, f.err
}

func (f *fakeTwoFactor) SetupTwoFactor(_ context.Context, id uuid.UUID) (authdomain.TwoFactorSetup, error) {
	f.user = id
	return authdomain.TwoFactorSetup{Secret: "SECRET", OTPAuthURL: "otpauth://totp/x"}, f.err
}

func (f *fakeTwoFactor) ConfirmTwoFactor(_ context.Context, id uuid.UUID, code string) ([]string, error) {
	f.user, f.code = id, code
	return []string{"aaaaa-bbbbb"}, f.err
}

func (f *fakeTwoFactor) DisableTwoFactor(_ context.Context, id uuid.UUID, password, code string) error {
	f.user, f.password, f.code = id, password, code
	return f.err
}

func (f *fakeTwoFactor) RegenerateBackupCodes(_ context.Context, id uuid.UUID, code string) ([]string, error) {
	f.user, f.code = id, code
	return []string{"ccccc-ddddd"}, f.err
}

// loginOnly satisfies authports.Service for loginHandler; other methods are unused.
type loginOnly struct {
	authports.Service

	result authdomain.LoginResult
}

func (l loginOnly) Login(context.Context, string, string, string, string, bool) (authdomain.LoginResult, error) {
	return l.result, nil
}

func TestLoginReturnsChallengeForTwoFactorAccounts(t *testing.T) {
	t.Parallel()

	auth := loginOnly{result: authdomain.LoginResult{Challenge: &authdomain.TwoFactorChallenge{
		Token: "chal", ExpiresAt: time.Now().Add(5 * time.Minute),
	}}}
	w, body := runProfile(t, loginHandler(auth), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/login", contentType: "application/json",
		body: `{"email":"a@example.com","password":"x"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())

	data := dataOf(body)
	require.Equal(t, true, data["two_factor_required"])
	require.Equal(t, "chal", data["challenge_token"])
	require.InDelta(t, 300, data["expires_in"], 2)
	require.NotContains(t, data, "access_token")
}

func TestTwoFactorChallengeReturnsTokens(t *testing.T) {
	t.Parallel()

	mfa := &fakeTwoFactor{}
	w, body := runProfile(t, twoFactorChallengeHandler(mfa), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/2fa/challenge", contentType: "application/json",
		body: `{"challenge_token":"chal","code":"123456"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "access", dataOf(body)["access_token"])
	require.Equal(t, "chal", mfa.token)
	require.Equal(t, "123456", mfa.code)
}

func TestTwoFactorChallengeMapsErrors(t *testing.T) {
	t.Parallel()

	cases := map[error]struct {
		status int
		code   string
	}{
		authdomain.ErrTwoFactorInvalidCode: {nethttp.StatusUnauthorized, "auth.2fa.invalid_code"},
		authdomain.ErrChallengeInvalid:     {nethttp.StatusUnauthorized, "auth.2fa.challenge_invalid"},
		authdomain.ErrTwoFactorUnavailable: {nethttp.StatusServiceUnavailable, "auth.2fa.unavailable"},
	}
	for err, want := range cases {
		w, body := runProfile(t, twoFactorChallengeHandler(&fakeTwoFactor{err: err}), profileRequest{
			method: nethttp.MethodPost, target: "/api/v1/auth/2fa/challenge", contentType: "application/json",
			body: `{"challenge_token":"chal","code":"123456"}`,
		})
		require.Equal(t, want.status, w.Code, w.Body.String())
		require.Equal(t, want.code, errorCode(body))
	}

	w, _ := runProfile(t, twoFactorChallengeHandler(&fakeTwoFactor{}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/2fa/challenge", contentType: "application/json",
		body: `{"code":"123456"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
}

func TestMeTwoFactorEndpoints(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	mfa := &fakeTwoFactor{}

	w, body := runProfile(t, meTwoFactorGetHandler(mfa), profileRequest{method: nethttp.MethodGet, target: "/api/v1/me/2fa", user: &user})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, true, dataOf(body)["enabled"])
	require.InDelta(t, 7, dataOf(body)["backup_codes_remaining"], 0)
	require.Equal(t, user, mfa.user)

	w, body = runProfile(t, meTwoFactorSetupHandler(mfa), profileRequest{method: nethttp.MethodPost, target: "/api/v1/me/2fa/setup", user: &user})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "SECRET", dataOf(body)["secret"])
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	w, body = runProfile(t, meTwoFactorConfirmHandler(mfa), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/2fa/confirm", contentType: "application/json",
		body: `{"code":"654321"}`, user: &user,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{"aaaaa-bbbbb"}, dataOf(body)["backup_codes"])
	require.Equal(t, "654321", mfa.code)

	w, body = runProfile(t, meTwoFactorBackupCodesHandler(mfa), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/2fa/backup-codes", contentType: "application/json",
		body: `{"code":"111111"}`, user: &user,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{"ccccc-ddddd"}, dataOf(body)["backup_codes"])

	w, body = runProfile(t, meTwoFactorDisableHandler(mfa), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/me/2fa", contentType: "application/json",
		body: `{"password":"pw","code":"222222"}`, user: &user,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, false, dataOf(body)["enabled"])
	require.Equal(t, "pw", mfa.password)

	w, _ = runProfile(t, meTwoFactorSetupHandler(mfa), profileRequest{method: nethttp.MethodPost, target: "/api/v1/me/2fa/setup"})
	require.Equal(t, nethttp.StatusUnauthorized, w.Code)

	w, body = runProfile(t, meTwoFactorSetupHandler(&fakeTwoFactor{err: authdomain.ErrTwoFactorAlreadyEnabled}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/2fa/setup", user: &user,
	})
	require.Equal(t, nethttp.StatusConflict, w.Code)
	require.Equal(t, "auth.2fa.already_enabled", errorCode(body))
}

type fakeAdminLogin struct {
	email string
	err   error
}

func (f *fakeAdminLogin) AdminLogin(_ context.Context, email, _, _, _ string, _ bool) (authdomain.LoginResult, error) {
	f.email = email
	return authdomain.LoginResult{Tokens: authdomain.TokenPair{AccessToken: "staff-token"}}, f.err
}

func TestAdminLoginHandler(t *testing.T) {
	t.Parallel()

	admin := &fakeAdminLogin{}
	w, body := runProfile(t, adminLoginHandler(admin), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/auth/login", contentType: "application/json",
		body: `{"email":"staff@example.com","password":"x"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "staff-token", dataOf(body)["access_token"])
	require.Equal(t, "staff@example.com", admin.email)

	w, body = runProfile(t, adminLoginHandler(&fakeAdminLogin{err: authdomain.ErrInvalidCredentials}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/admin/auth/login", contentType: "application/json",
		body: `{"email":"reader@example.com","password":"x"}`,
	})
	require.Equal(t, nethttp.StatusUnauthorized, w.Code)
	require.Equal(t, "unauthorized", errorCode(body))
}
