package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

type fakeOAuth struct {
	provider, redirectURI, code, state string
	result                             authdomain.LoginResult
	err                                error
}

func (f *fakeOAuth) OAuthStart(_ context.Context, provider, redirectURI string) (authdomain.OAuthStart, error) {
	f.provider, f.redirectURI = provider, redirectURI

	return authdomain.OAuthStart{
		AuthorizeURL: "https://provider.test/authorize?state=s",
		ExpiresAt:    time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}, f.err
}

func (f *fakeOAuth) OAuthCallback(_ context.Context, provider, code, state, _, _ string) (authdomain.LoginResult, error) {
	f.provider, f.code, f.state = provider, code, state
	return f.result, f.err
}

func TestOAuthStartHandler(t *testing.T) {
	t.Parallel()

	api := &fakeOAuth{}
	w, body := runProfile(t, oauthStartHandler(api), profileRequest{
		method: nethttp.MethodGet, param: "google",
		target: "/api/v1/auth/oauth/google/start?redirect_uri=https%3A%2F%2Fapp.test%2Fcb",
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "https://provider.test/authorize?state=s", dataOf(body)["authorize_url"])
	require.Equal(t, "2026-09-25T12:00:00Z", dataOf(body)["expires_at"])
	require.Equal(t, "google", api.provider)
	require.Equal(t, "https://app.test/cb", api.redirectURI)
}

func TestOAuthStartHandlerErrors(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, oauthStartHandler(&fakeOAuth{}), profileRequest{
		method: nethttp.MethodGet, param: "google", target: "/api/v1/auth/oauth/google/start",
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	cases := map[error]int{
		authdomain.ErrOAuthProviderUnknown: nethttp.StatusNotFound,
		authdomain.ErrOAuthRedirectURI:     nethttp.StatusBadRequest,
	}
	for err, status := range cases {
		w, _ := runProfile(t, oauthStartHandler(&fakeOAuth{err: err}), profileRequest{
			method: nethttp.MethodGet, param: "gitlab", target: "/api/v1/auth/oauth/gitlab/start?redirect_uri=x",
		})
		require.Equal(t, status, w.Code, err.Error())
	}
}

func TestOAuthCallbackHandler(t *testing.T) {
	t.Parallel()

	api := &fakeOAuth{result: authdomain.LoginResult{Tokens: authdomain.TokenPair{AccessToken: "oauth-token", TokenType: "Bearer"}}}
	w, body := runProfile(t, oauthCallbackHandler(api), profileRequest{
		method: nethttp.MethodPost, param: "github", target: "/api/v1/auth/oauth/github/callback",
		contentType: "application/json", body: `{"code":"c1","state":"s1"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "oauth-token", dataOf(body)["access_token"])
	require.Equal(t, "github", api.provider)
	require.Equal(t, "c1", api.code)
	require.Equal(t, "s1", api.state)

	api = &fakeOAuth{result: authdomain.LoginResult{Challenge: &authdomain.TwoFactorChallenge{
		Token: "challenge", ExpiresAt: time.Now().Add(5 * time.Minute),
	}}}
	w, body = runProfile(t, oauthCallbackHandler(api), profileRequest{
		method: nethttp.MethodPost, param: "github", target: "/api/v1/auth/oauth/github/callback",
		contentType: "application/json", body: `{"code":"c1","state":"s1"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, true, dataOf(body)["two_factor_required"])
	require.Equal(t, "challenge", dataOf(body)["challenge_token"])
}

func TestOAuthCallbackHandlerErrors(t *testing.T) {
	t.Parallel()

	w, _ := runProfile(t, oauthCallbackHandler(&fakeOAuth{}), profileRequest{
		method: nethttp.MethodPost, param: "github", target: "/api/v1/auth/oauth/github/callback",
		contentType: "application/json", body: `{"code":"c1"}`,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	cases := map[error]struct {
		status int
		code   string
	}{
		authdomain.ErrOAuthNoAccount:    {nethttp.StatusUnauthorized, "auth.oauth.no_account"},
		authdomain.ErrOAuthStateInvalid: {nethttp.StatusUnauthorized, "auth.oauth.state_invalid"},
		authdomain.ErrOAuthLinkConflict: {nethttp.StatusConflict, "auth.oauth.link_conflict"},
	}
	for err, want := range cases {
		w, body := runProfile(t, oauthCallbackHandler(&fakeOAuth{err: err}), profileRequest{
			method: nethttp.MethodPost, param: "github", target: "/api/v1/auth/oauth/github/callback",
			contentType: "application/json", body: `{"code":"c1","state":"s1"}`,
		})
		require.Equal(t, want.status, w.Code, err.Error())
		require.Equal(t, want.code, errorCode(body), err.Error())
	}
}
