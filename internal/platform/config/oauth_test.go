package config

import (
	"slices"
	"strings"
	"testing"
)

func TestLoadOAuthDisabledByDefault(t *testing.T) {
	setJWTKeys(t)

	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "")
	t.Setenv("OAUTH_REDIRECT_URIS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.OAuthEnabled() || len(cfg.OAuthRedirectURIs) != 0 {
		t.Fatalf("oauth enabled=%v redirects=%v", cfg.OAuthEnabled(), cfg.OAuthRedirectURIs)
	}
}

func TestLoadOAuthCredentialsAndRedirects(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "gh-id")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "gh-secret")
	t.Setenv("OAUTH_REDIRECT_URIS", "https://app.example.com/oauth/callback, http://localhost:5173/oauth/callback")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"https://app.example.com/oauth/callback", "http://localhost:5173/oauth/callback"}
	if !cfg.OAuthEnabled() || !slices.Equal(cfg.OAuthRedirectURIs, want) {
		t.Fatalf("enabled=%v redirects=%v", cfg.OAuthEnabled(), cfg.OAuthRedirectURIs)
	}
}

func TestValidateOAuth(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cfg  Config
		want string
	}{
		"id without secret": {Config{OAuthGoogleClientID: "id"}, "OAUTH_GOOGLE_CLIENT_ID"},
		"secret without id": {Config{OAuthGitHubClientSecret: "s"}, "OAUTH_GITHUB_CLIENT_ID"},
		"relative redirect": {Config{OAuthRedirectURIs: []string{"/cb"}}, "OAUTH_REDIRECT_URIS"},
		"custom scheme":     {Config{OAuthRedirectURIs: []string{"javascript://x/cb"}}, "OAUTH_REDIRECT_URIS"},
		"fragment":          {Config{OAuthRedirectURIs: []string{"https://a.test/cb#x"}}, "OAUTH_REDIRECT_URIS"},
		"http in production": {
			Config{Environment: "production", OAuthRedirectURIs: []string{"http://a.test/cb"}}, "https",
		},
		"valid": {Config{
			Environment: "production", OAuthGoogleClientID: "id", OAuthGoogleClientSecret: "s",
			OAuthRedirectURIs: []string{"https://a.test/cb"},
		}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.ValidateOAuth()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
}
