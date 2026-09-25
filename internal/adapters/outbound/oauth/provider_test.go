package oauth_test

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/outbound/oauth"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

var creds = oauth.Credentials{ClientID: "client-id", ClientSecret: "client-secret"}

// fakeServer serves a token endpoint that accepts code "good" with verifier
// "verifier", and bearer-protected JSON for every other path.
func fakeServer(t *testing.T, tokenStatus int, resources map[string]any) (*httptest.Server, oauth.Endpoints) {
	t.Helper()

	mux := nethttp.NewServeMux()
	mux.HandleFunc("POST /token", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !assertForm(t, r) {
			w.WriteHeader(nethttp.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request"}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(tokenStatus)

		if r.PostForm.Get("code") != "good" {
			_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
			return
		}

		_, _ = w.Write([]byte(`{"access_token":"access-123","token_type":"bearer"}`))
	})

	for path, body := range resources {
		mux.HandleFunc("GET "+path, func(w nethttp.ResponseWriter, r *nethttp.Request) {
			if r.Header.Get("Authorization") != "Bearer access-123" {
				w.WriteHeader(nethttp.StatusUnauthorized)
				return
			}

			_ = json.NewEncoder(w).Encode(body)
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, oauth.Endpoints{
		Authorize: srv.URL + "/authorize", Token: srv.URL + "/token",
		UserInfo: srv.URL + "/user", Emails: srv.URL + "/user/emails",
	}
}

func assertForm(t *testing.T, r *nethttp.Request) bool {
	t.Helper()

	if err := r.ParseForm(); err != nil {
		return false
	}

	return r.PostForm.Get("grant_type") == "authorization_code" &&
		r.PostForm.Get("client_id") == creds.ClientID &&
		r.PostForm.Get("client_secret") == creds.ClientSecret &&
		r.PostForm.Get("code_verifier") == "verifier" &&
		r.PostForm.Get("redirect_uri") == "https://app.test/cb"
}

func TestAuthorizeURLCarriesPKCE(t *testing.T) {
	t.Parallel()

	raw := oauth.NewGitHub(creds, oauth.GitHubEndpoints).AuthorizeURL("state-1", "challenge-1", "https://app.test/cb")

	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "github.com", u.Host)

	q := u.Query()
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "client-id", q.Get("client_id"))
	require.Equal(t, "state-1", q.Get("state"))
	require.Equal(t, "challenge-1", q.Get("code_challenge"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Equal(t, "https://app.test/cb", q.Get("redirect_uri"))
	require.Empty(t, q.Get("client_secret"))
}

func TestGoogleExchange(t *testing.T) {
	t.Parallel()

	_, endpoints := fakeServer(t, nethttp.StatusOK, map[string]any{
		"/user": map[string]any{"sub": "g-42", "email": "ada@example.com", "email_verified": true},
	})
	p := oauth.NewGoogle(creds, endpoints)

	identity, err := p.Exchange(t.Context(), "good", "verifier", "https://app.test/cb")
	require.NoError(t, err)
	require.Equal(t, authdomain.OAuthIdentity{
		Provider: oauth.Google, Subject: "g-42", Email: "ada@example.com", EmailVerified: true,
	}, identity)
}

func TestGoogleExchangeRejectsBadCode(t *testing.T) {
	t.Parallel()

	_, endpoints := fakeServer(t, nethttp.StatusBadRequest, nil)

	_, err := oauth.NewGoogle(creds, endpoints).Exchange(t.Context(), "bad", "verifier", "https://app.test/cb")
	require.ErrorIs(t, err, authdomain.ErrOAuthExchange)

	_, err = oauth.NewGoogle(creds, endpoints).Exchange(t.Context(), "", "verifier", "https://app.test/cb")
	require.ErrorIs(t, err, authdomain.ErrOAuthExchange)
}

func TestGitHubExchangeUsesPrimaryVerifiedEmail(t *testing.T) {
	t.Parallel()

	_, endpoints := fakeServer(t, nethttp.StatusOK, map[string]any{
		"/user": map[string]any{"id": 9001, "login": "ada"},
		"/user/emails": []map[string]any{
			{"email": "old@example.com", "primary": false, "verified": true},
			{"email": "ada@example.com", "primary": true, "verified": true},
		},
	})

	identity, err := oauth.NewGitHub(creds, endpoints).Exchange(t.Context(), "good", "verifier", "https://app.test/cb")
	require.NoError(t, err)
	require.Equal(t, authdomain.OAuthIdentity{
		Provider: oauth.GitHub, Subject: "9001", Email: "ada@example.com", EmailVerified: true,
	}, identity)
}

func TestGitHubExchangeIgnoresUnverifiedPrimary(t *testing.T) {
	t.Parallel()

	_, endpoints := fakeServer(t, nethttp.StatusOK, map[string]any{
		"/user":        map[string]any{"id": 7},
		"/user/emails": []map[string]any{{"email": "ada@example.com", "primary": true, "verified": false}},
	})

	identity, err := oauth.NewGitHub(creds, endpoints).Exchange(t.Context(), "good", "verifier", "https://app.test/cb")
	require.NoError(t, err)
	require.Equal(t, "7", identity.Subject)
	require.Empty(t, identity.Email)
	require.False(t, identity.EmailVerified)
}

func TestGitHubExchangeTreatsErrorFieldAsFailure(t *testing.T) {
	t.Parallel()

	// GitHub answers a bad code with 200 and an "error" field.
	_, endpoints := fakeServer(t, nethttp.StatusOK, nil)

	_, err := oauth.NewGitHub(creds, endpoints).Exchange(t.Context(), "bad", "verifier", "https://app.test/cb")
	require.ErrorIs(t, err, authdomain.ErrOAuthExchange)
}

func TestExchangeTransportFailureIsNotBlamedOnUser(t *testing.T) {
	t.Parallel()

	srv, endpoints := fakeServer(t, nethttp.StatusOK, nil)
	srv.Close()

	_, err := oauth.NewGoogle(creds, endpoints).Exchange(t.Context(), "good", "verifier", "https://app.test/cb")
	require.Error(t, err)
	require.NotErrorIs(t, err, authdomain.ErrOAuthExchange)
}
