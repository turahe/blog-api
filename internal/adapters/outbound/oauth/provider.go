// Package oauth implements the Google and GitHub authorization code flow with PKCE.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
)

// Provider names used in /api/v1/auth/oauth/{provider}/….
const (
	Google = "google"
	GitHub = "github"
)

const maxResponseBytes = 1 << 20

// Endpoints are a provider's URLs; tests point them at a fake server.
type Endpoints struct {
	Authorize string
	Token     string
	UserInfo  string
	Emails    string // GitHub only
}

// GoogleEndpoints are Google's OpenID Connect endpoints.
//
//nolint:gosec // public endpoint URLs, not credentials
var GoogleEndpoints = Endpoints{
	Authorize: "https://accounts.google.com/o/oauth2/v2/auth",
	Token:     "https://oauth2.googleapis.com/token",
	UserInfo:  "https://openidconnect.googleapis.com/v1/userinfo",
}

// GitHubEndpoints are GitHub's OAuth app endpoints.
//
//nolint:gosec // public endpoint URLs, not credentials
var GitHubEndpoints = Endpoints{
	Authorize: "https://github.com/login/oauth/authorize",
	Token:     "https://github.com/login/oauth/access_token",
	UserInfo:  "https://api.github.com/user",
	Emails:    "https://api.github.com/user/emails",
}

// Credentials are an OAuth client's id and secret.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// Provider is one configured OAuth provider.
type Provider struct {
	name      string
	creds     Credentials
	endpoints Endpoints
	scopes    string
	identify  func(ctx context.Context, p *Provider, accessToken string) (authdomain.OAuthIdentity, error)
	client    *nethttp.Client
}

var _ authports.OAuthProvider = (*Provider)(nil)

// NewGoogle returns the Google provider.
func NewGoogle(creds Credentials, endpoints Endpoints) *Provider {
	return newProvider(Google, creds, endpoints, "openid email profile", googleIdentity)
}

// NewGitHub returns the GitHub provider.
func NewGitHub(creds Credentials, endpoints Endpoints) *Provider {
	return newProvider(GitHub, creds, endpoints, "read:user user:email", githubIdentity)
}

func newProvider(
	name string, creds Credentials, endpoints Endpoints, scopes string,
	identify func(context.Context, *Provider, string) (authdomain.OAuthIdentity, error),
) *Provider {
	return &Provider{
		name: name, creds: creds, endpoints: endpoints, scopes: scopes, identify: identify,
		client: &nethttp.Client{Timeout: 10 * time.Second},
	}
}

// AuthorizeURL implements authports.OAuthProvider.
func (p *Provider) AuthorizeURL(state, codeChallenge, redirectURI string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.creds.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", p.scopes)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")

	return p.endpoints.Authorize + "?" + q.Encode()
}

// Exchange implements authports.OAuthProvider.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (authdomain.OAuthIdentity, error) {
	if code == "" {
		return authdomain.OAuthIdentity{}, authdomain.ErrOAuthExchange
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", p.creds.ClientID)
	form.Set("client_secret", p.creds.ClientSecret)
	form.Set("code_verifier", codeVerifier)

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, p.endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}

	status, err := p.do(req, &token)
	if err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	// GitHub reports a bad code as 200 with an "error" field.
	if status >= 400 || token.Error != "" || token.AccessToken == "" {
		return authdomain.OAuthIdentity{}, fmt.Errorf("%w: %s token endpoint: status %d %s", authdomain.ErrOAuthExchange, p.name, status, token.Error)
	}

	identity, err := p.identify(ctx, p, token.AccessToken)
	if err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	identity.Provider = p.name

	return identity, nil
}

func googleIdentity(ctx context.Context, p *Provider, accessToken string) (authdomain.OAuthIdentity, error) {
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := p.getJSON(ctx, p.endpoints.UserInfo, accessToken, &info); err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	if info.Sub == "" {
		return authdomain.OAuthIdentity{}, fmt.Errorf("%w: google userinfo without sub", authdomain.ErrOAuthExchange)
	}

	return authdomain.OAuthIdentity{Subject: info.Sub, Email: info.Email, EmailVerified: info.EmailVerified}, nil
}

func githubIdentity(ctx context.Context, p *Provider, accessToken string) (authdomain.OAuthIdentity, error) {
	var user struct {
		ID int64 `json:"id"`
	}
	if err := p.getJSON(ctx, p.endpoints.UserInfo, accessToken, &user); err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	if user.ID == 0 {
		return authdomain.OAuthIdentity{}, fmt.Errorf("%w: github user without id", authdomain.ErrOAuthExchange)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := p.getJSON(ctx, p.endpoints.Emails, accessToken, &emails); err != nil {
		return authdomain.OAuthIdentity{}, err
	}

	identity := authdomain.OAuthIdentity{Subject: strconv.FormatInt(user.ID, 10)}

	for _, e := range emails {
		if e.Primary && e.Verified {
			identity.Email, identity.EmailVerified = e.Email, true
		}
	}

	return identity, nil
}

func (p *Provider) getJSON(ctx context.Context, endpoint, accessToken string, out any) error {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	status, err := p.do(req, out)
	if err != nil {
		return err
	}

	if status >= 400 {
		return fmt.Errorf("%w: %s %s: status %d", authdomain.ErrOAuthExchange, p.name, endpoint, status)
	}

	return nil
}

// do sends req and decodes a JSON body into out. Transport failures are returned
// as-is (a 500), not as ErrOAuthExchange, so provider outages are not blamed on the user.
func (p *Provider) do(req *nethttp.Request, out any) (int, error) {
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s request: %w", p.name, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("%s response: %w", p.name, err)
	}

	if len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil && resp.StatusCode < 400 {
			return 0, fmt.Errorf("%s response: %w", p.name, errors.Join(authdomain.ErrOAuthExchange, err))
		}
	}

	return resp.StatusCode, nil
}
