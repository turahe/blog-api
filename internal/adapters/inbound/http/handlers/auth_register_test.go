package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeRegistrar struct {
	signUp          authdomain.SignUp
	token, password string
	pair            authdomain.TokenPair
	err             error
}

func (f *fakeRegistrar) Register(_ context.Context, in authdomain.SignUp) error {
	f.signUp = in
	return f.err
}

func (f *fakeRegistrar) VerifyEmail(_ context.Context, token, password, _, _ string) (authdomain.TokenPair, error) {
	f.token, f.password = token, password
	return f.pair, f.err
}

const registerBody = `{"email":"new@example.com","username":"reader","full_name":"New Reader","password":"Sup3rSecretPass"}`

func TestRegisterHandlerAccepts(t *testing.T) {
	t.Parallel()

	api := &fakeRegistrar{}
	w, _ := runProfile(t, registerHandler(api), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/register", contentType: "application/json", body: registerBody,
	})
	require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
	require.Equal(t, authdomain.SignUp{
		Email: "new@example.com", Username: "reader", FullName: "New Reader", Password: "Sup3rSecretPass",
	}, api.signUp)
}

func TestRegisterHandlerErrors(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, registerHandler(&fakeRegistrar{}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/register", contentType: "application/json",
		body: `{"email":"not-an-email","username":"reader","full_name":"R","password":"Sup3rSecretPass"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	for err, want := range map[error]struct {
		status int
		code   string
	}{
		authdomain.ErrRegistrationClosed: {nethttp.StatusForbidden, "auth.registration.closed"},
		userdomain.ErrUsernameTaken:      {nethttp.StatusConflict, "user.username.taken"},
		authdomain.ErrPasswordStrength:   {nethttp.StatusUnprocessableEntity, "password.strength"},
	} {
		w, _ := runProfile(t, registerHandler(&fakeRegistrar{err: err}), profileRequest{
			method: nethttp.MethodPost, target: "/api/v1/auth/register", contentType: "application/json", body: registerBody,
		})
		require.Equal(t, want.status, w.Code, err.Error())
		require.Equal(t, want.code, errorCodeOf(t, w), err.Error())
	}
}

func TestVerifyEmailHandlerSignsIn(t *testing.T) {
	t.Parallel()

	api := &fakeRegistrar{pair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 900}}
	w, body := runProfile(t, verifyEmailHandler(api), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/verify-email", contentType: "application/json",
		body: `{"token":"tok","password":"Sup3rSecretPass"}`,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "access", dataOf(body)["access_token"])
	require.Equal(t, "refresh", dataOf(body)["refresh_token"])
	require.Equal(t, "tok", api.token)
	require.Equal(t, "Sup3rSecretPass", api.password)
}

func TestVerifyEmailHandlerErrors(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, verifyEmailHandler(&fakeRegistrar{}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/verify-email", contentType: "application/json", body: `{"token":"tok"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	for err, want := range map[error]struct {
		status int
		code   string
	}{
		authdomain.ErrRegistrationTokenInvalid: {nethttp.StatusBadRequest, "auth.registration.token_invalid"},
		authdomain.ErrRegistrationTokenExpired: {nethttp.StatusBadRequest, "auth.registration.token_expired"},
		authdomain.ErrInvalidCredentials:       {nethttp.StatusUnauthorized, "unauthorized"},
		authdomain.ErrEmailTaken:               {nethttp.StatusConflict, "auth.email.taken"},
		authdomain.ErrRegistrationClosed:       {nethttp.StatusForbidden, "auth.registration.closed"},
	} {
		w, _ := runProfile(t, verifyEmailHandler(&fakeRegistrar{err: err}), profileRequest{
			method: nethttp.MethodPost, target: "/api/v1/auth/verify-email", contentType: "application/json",
			body: `{"token":"tok","password":"Sup3rSecretPass"}`,
		})
		require.Equal(t, want.status, w.Code, err.Error())
		require.Equal(t, want.code, errorCodeOf(t, w), err.Error())
	}

	w, _ = runProfile(t, verifyEmailHandler(&fakeRegistrar{err: authdomain.LockedError{RetryAfter: 90 * time.Second}}), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/auth/verify-email", contentType: "application/json",
		body: `{"token":"tok","password":"Sup3rSecretPass"}`,
	})
	require.Equal(t, nethttp.StatusTooManyRequests, w.Code)
	require.Equal(t, "90", w.Header().Get("Retry-After"))
}
