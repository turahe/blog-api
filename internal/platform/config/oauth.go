package config

import (
	"fmt"
	"net/url"
)

// ValidateOAuth requires both halves of each client credential and absolute
// redirect URIs (https in production).
func (c Config) ValidateOAuth() error {
	for name, pair := range map[string][2]string{
		"OAUTH_GOOGLE": {c.OAuthGoogleClientID, c.OAuthGoogleClientSecret},
		"OAUTH_GITHUB": {c.OAuthGitHubClientID, c.OAuthGitHubClientSecret},
	} {
		if (pair[0] == "") != (pair[1] == "") {
			return fmt.Errorf("%s_CLIENT_ID and %s_CLIENT_SECRET must be set together", name, name)
		}
	}

	for _, raw := range c.OAuthRedirectURIs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Fragment != "" {
			return fmt.Errorf("OAUTH_REDIRECT_URIS: %q is not an absolute http(s) URL without a fragment", raw)
		}

		if c.Environment == envProduction && u.Scheme != "https" {
			return fmt.Errorf("OAUTH_REDIRECT_URIS: %q must use https in production", raw)
		}
	}

	return nil
}

// OAuthEnabled reports whether any provider is configured.
func (c Config) OAuthEnabled() bool {
	return c.OAuthGoogleClientID != "" || c.OAuthGitHubClientID != ""
}
