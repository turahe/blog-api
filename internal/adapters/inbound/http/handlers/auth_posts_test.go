package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

// fakeAuth is an authports.Service recording the arguments of the last call.
type fakeAuth struct {
	err error

	loginResult authdomain.LoginResult
	pair        authdomain.TokenPair
	validity    authdomain.ResetTokenValidity
	changedAt   time.Time
	invalidated bool

	user                      uuid.UUID
	token, identifier         string
	current, next, confirm    string
	revokeAll                 bool
	loginEmail, loginPassword string
}

func (f *fakeAuth) Login(_ context.Context, email, password, _, _ string, _ bool) (authdomain.LoginResult, error) {
	f.loginEmail, f.loginPassword = email, password
	return f.loginResult, f.err
}

func (f *fakeAuth) Refresh(_ context.Context, refreshToken, _, _ string) (authdomain.TokenPair, error) {
	f.token = refreshToken
	return f.pair, f.err
}

func (f *fakeAuth) Logout(_ context.Context, userID uuid.UUID, refreshToken string) error {
	f.user, f.token = userID, refreshToken
	return f.err
}

func (f *fakeAuth) ParseAccessToken(string) (authdomain.AccessClaims, error) {
	return authdomain.AccessClaims{}, f.err
}

func (f *fakeAuth) ForgotPassword(_ context.Context, emailOrUsername string) error {
	f.identifier = emailOrUsername
	return f.err
}

func (f *fakeAuth) CheckResetToken(_ context.Context, rawToken string) (authdomain.ResetTokenValidity, error) {
	f.token = rawToken
	return f.validity, f.err
}

func (f *fakeAuth) ResetPassword(_ context.Context, rawToken, newPassword, confirmPassword string) error {
	f.token, f.next, f.confirm = rawToken, newPassword, confirmPassword
	return f.err
}

func (f *fakeAuth) ChangePassword(_ context.Context, userID uuid.UUID, current, newPassword, confirm string, revokeAll bool) (time.Time, bool, error) {
	f.user, f.current, f.next, f.confirm, f.revokeAll = userID, current, newPassword, confirm, revokeAll
	return f.changedAt, f.invalidated, f.err
}

const jsonContent = "application/json"

func TestLoginHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{name: "tokens", body: `{"email":"a@example.com","password":"pw"}`, status: nethttp.StatusOK},
		{name: "malformed body", body: `{`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{
			name: "locked rounds retry-after up", body: `{"email":"a@example.com","password":"pw"}`,
			err:    authdomain.LockedError{RetryAfter: 1500 * time.Millisecond},
			status: nethttp.StatusTooManyRequests, code: "auth.login.locked", retryAfter: "2",
		},
		{
			name: "locked retry-after is at least one second", body: `{"email":"a@example.com","password":"pw"}`,
			err:    authdomain.LockedError{},
			status: nethttp.StatusTooManyRequests, code: "auth.login.locked", retryAfter: "1",
		},
		{
			name: "invalid credentials", body: `{"email":"a@example.com","password":"pw"}`,
			err: authdomain.ErrInvalidCredentials, status: nethttp.StatusUnauthorized, code: "unauthorized",
		},
		{
			name: "unexpected error", body: `{"email":"a@example.com","password":"pw"}`,
			err: errors.New("db down"), status: nethttp.StatusInternalServerError, code: "internal_error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err, loginResult: authdomain.LoginResult{Tokens: authdomain.TokenPair{AccessToken: "acc"}}}
			w, body := runProfile(t, loginHandler(auth), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/auth/login", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.retryAfter, w.Header().Get("Retry-After"))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, "acc", dataOf(body)["accessToken"])
				require.Equal(t, "a@example.com", auth.loginEmail)
			}
		})
	}
}

func TestRefreshHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "rotates the pair", body: `{"refreshToken":"rt"}`, status: nethttp.StatusOK},
		{name: "missing token", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "revoked token", body: `{"refreshToken":"rt"}`, err: authdomain.ErrTokenRevoked, status: nethttp.StatusUnauthorized, code: "unauthorized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err, pair: authdomain.TokenPair{AccessToken: "new-access", RefreshToken: "new-refresh"}}
			w, body := runProfile(t, refreshHandler(auth), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/auth/refresh", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, "new-access", dataOf(body)["accessToken"])
				require.Equal(t, "new-refresh", dataOf(body)["refreshToken"])
				require.Equal(t, "rt", auth.token)
			}
		})
	}
}

func TestLogoutHandler(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	tests := []struct {
		name      string
		user      *uuid.UUID
		body      string
		err       error
		status    int
		code      string
		wantToken string
	}{
		{name: "anonymous", body: `{}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "empty body revokes the session", user: &user, status: nethttp.StatusOK},
		{name: "revokes the given refresh token", user: &user, body: `{"refreshToken":"rt"}`, status: nethttp.StatusOK, wantToken: "rt"},
		{name: "malformed body", user: &user, body: `{"refreshToken":`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "service failure", user: &user, body: `{}`, err: errors.New("redis down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err}
			w, body := runProfile(t, logoutHandler(auth), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/auth/logout", contentType: jsonContent, body: tc.body, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, user, auth.user)
				require.Equal(t, tc.wantToken, auth.token)
				require.Empty(t, dataOf(body))
			}
		})
	}
}

func TestForgotPasswordHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "accepted", body: `{"emailOrUsername":"ada"}`, status: nethttp.StatusAccepted},
		{name: "missing identifier", body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "service failure", body: `{"emailOrUsername":"ada"}`, err: errors.New("mailer down"), status: nethttp.StatusInternalServerError, code: "internal_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err}
			w, body := runProfile(t, forgotPasswordHandler(auth), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/auth/password/forgot", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusAccepted {
				require.Equal(t, "ada", auth.identifier)
			}
		})
	}
}

func TestResetTokenValidityHandler(t *testing.T) {
	t.Parallel()

	expires := time.Date(2026, 9, 1, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "valid token", status: nethttp.StatusOK},
		{name: "invalid token", err: authdomain.ErrInvalidToken, status: nethttp.StatusBadRequest, code: "auth.password.reset_token_invalid"},
		{name: "expired token", err: authdomain.ErrTokenExpired, status: nethttp.StatusBadRequest, code: "auth.password.reset_token_expired"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err, validity: authdomain.ResetTokenValidity{Valid: true, ExpiresAt: expires}}
			w, body := runProfile(t, resetTokenValidityHandler(auth), profileRequest{
				method: nethttp.MethodGet, target: "/api/v1/auth/password/reset/tok", param: "  tok  ",
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, "tok", auth.token)

			if tc.status == nethttp.StatusOK {
				require.Equal(t, true, dataOf(body)["valid"])
				require.Equal(t, "2026-09-01T03:00:00Z", dataOf(body)["expiresAt"])
			}
		})
	}
}

func TestResetPasswordHandler(t *testing.T) {
	t.Parallel()

	const valid = `{"token":"tok","newPassword":"Str0ngPassword!","confirmPassword":"Str0ngPassword!"}`

	tests := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "resets", body: valid, status: nethttp.StatusOK},
		{
			name: "confirmation mismatch", status: nethttp.StatusBadRequest, code: "validation_error",
			body: `{"token":"tok","newPassword":"Str0ngPassword!","confirmPassword":"other"}`,
		},
		{name: "used token", body: valid, err: authdomain.ErrTokenUsed, status: nethttp.StatusBadRequest, code: "auth.password.reset_token_used"},
		{name: "invalid token", body: valid, err: authdomain.ErrInvalidToken, status: nethttp.StatusBadRequest, code: "auth.password.reset_token_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err}
			w, body := runProfile(t, resetPasswordHandler(auth), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/auth/password/reset", contentType: jsonContent, body: tc.body,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, "tok", auth.token)
				require.Equal(t, "Str0ngPassword!", auth.next)
			}
		})
	}
}

func TestChangePasswordHandler(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	changedAt := time.Date(2026, 9, 2, 8, 30, 0, 0, time.FixedZone("WIB", 7*3600))

	tests := []struct {
		name          string
		user          *uuid.UUID
		body          string
		err           error
		status        int
		code          string
		wantRevokeAll bool
	}{
		{name: "anonymous", body: `{}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{
			name: "revokes all sessions by default", user: &user, status: nethttp.StatusOK, wantRevokeAll: true,
			body: `{"currentPassword":"old","newPassword":"Str0ngPassword!","confirmPassword":"Str0ngPassword!"}`,
		},
		{
			name: "keeps other sessions when asked", user: &user, status: nethttp.StatusOK, wantRevokeAll: false,
			body: `{"currentPassword":"old","newPassword":"Str0ngPassword!","confirmPassword":"Str0ngPassword!","revokeAllSessions":false}`,
		},
		{name: "missing fields", user: &user, body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{
			name: "wrong current password", user: &user, err: authdomain.ErrCurrentPassword,
			status: nethttp.StatusForbidden, code: "password.current_mismatch",
			body: `{"currentPassword":"bad","newPassword":"Str0ngPassword!","confirmPassword":"Str0ngPassword!"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{err: tc.err, changedAt: changedAt, invalidated: true}
			w, body := runProfile(t, changePasswordHandler(auth), profileRequest{
				method: nethttp.MethodPut, target: "/api/v1/me/password", contentType: jsonContent, body: tc.body, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, user, auth.user)
				require.Equal(t, "old", auth.current)
				require.Equal(t, tc.wantRevokeAll, auth.revokeAll)
				require.Equal(t, "2026-09-02T01:30:00Z", dataOf(body)["passwordChangedAt"])
				require.Equal(t, true, dataOf(body)["sessionsInvalidated"])
			}
		})
	}
}
