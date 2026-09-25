package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const oauthStateTTL = 10 * time.Minute

type oauthDeps struct {
	providers    map[string]ports.OAuthProvider
	identities   ports.OAuthIdentityRepository
	states       ports.OAuthStateStore
	redirectURIs []string
}

// WithOAuth enables social login for the configured providers. Only redirect
// URIs listed exactly in redirectURIs may be used.
func (s *AuthService) WithOAuth(
	providers map[string]ports.OAuthProvider, identities ports.OAuthIdentityRepository,
	states ports.OAuthStateStore, redirectURIs []string,
) *AuthService {
	s.oauth = oauthDeps{providers: providers, identities: identities, states: states, redirectURIs: redirectURIs}
	return s
}

// OAuthStart begins an authorization request: it stores a single-use state with
// a PKCE verifier and returns the provider URL to redirect the browser to.
func (s *AuthService) OAuthStart(ctx context.Context, provider, redirectURI string) (authdomain.OAuthStart, error) {
	p, err := s.oauthProvider(provider)
	if err != nil {
		return authdomain.OAuthStart{}, err
	}

	if !slices.Contains(s.oauth.redirectURIs, redirectURI) {
		return authdomain.OAuthStart{}, authdomain.ErrOAuthRedirectURI
	}

	state, err := randomToken()
	if err != nil {
		return authdomain.OAuthStart{}, err
	}

	verifier, err := randomToken()
	if err != nil {
		return authdomain.OAuthStart{}, err
	}

	value := authdomain.OAuthState{Provider: provider, RedirectURI: redirectURI, CodeVerifier: verifier}
	if err := s.oauth.states.Save(ctx, state, value, oauthStateTTL); err != nil {
		return authdomain.OAuthStart{}, fmt.Errorf("save oauth state: %w", err)
	}

	return authdomain.OAuthStart{
		AuthorizeURL: p.AuthorizeURL(state, pkceChallenge(verifier), redirectURI),
		ExpiresAt:    s.clock.Now().Add(oauthStateTTL),
	}, nil
}

// OAuthCallback completes an authorization request and signs in the linked
// account. Accounts are never created: an identity signs in only when it is
// already linked, or when the provider verified an email that matches an
// existing account (the link is then saved). Two-factor accounts get a challenge.
func (s *AuthService) OAuthCallback(ctx context.Context, provider, code, state, userAgent, ip string) (authdomain.LoginResult, error) {
	p, err := s.oauthProvider(provider)
	if err != nil {
		return authdomain.LoginResult{}, err
	}

	pending, err := s.oauth.states.Consume(ctx, strings.TrimSpace(state))
	if err != nil {
		return authdomain.LoginResult{}, err
	}

	if pending.Provider != provider {
		return authdomain.LoginResult{}, authdomain.ErrOAuthStateInvalid
	}

	identity, err := p.Exchange(ctx, strings.TrimSpace(code), pending.CodeVerifier, pending.RedirectURI)
	if err != nil {
		return authdomain.LoginResult{}, err
	}

	identity.Provider = provider

	user, err := s.oauthUser(ctx, identity)
	if err != nil {
		return authdomain.LoginResult{}, err
	}

	challenge, err := s.startTwoFactor(ctx, user.UUID, authdomain.PendingLogin{
		UserUUID: user.UUID, UserAgent: userAgent, IPAddress: ip,
	})
	if err != nil || challenge != nil {
		return authdomain.LoginResult{Challenge: challenge}, err
	}

	pair, err := s.completeLogin(ctx, user, userAgent, ip, false)

	return authdomain.LoginResult{Tokens: pair}, err
}

// oauthUser resolves the active account for identity, linking it by verified email when new.
func (s *AuthService) oauthUser(ctx context.Context, identity authdomain.OAuthIdentity) (userdomain.User, error) {
	now := s.clock.Now()

	userID, err := s.oauth.identities.FindUser(ctx, identity.Provider, identity.Subject)
	switch {
	case err == nil:
		if err := s.oauth.identities.Touch(ctx, identity.Provider, identity.Subject, now); err != nil {
			return userdomain.User{}, fmt.Errorf("touch oauth identity: %w", err)
		}
	case errors.Is(err, authdomain.ErrOAuthNoAccount):
		userID, err = s.linkByEmail(ctx, identity, now)
		if err != nil {
			return userdomain.User{}, err
		}
	default:
		return userdomain.User{}, err
	}

	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, userdomain.ErrNotFound) {
		return userdomain.User{}, authdomain.ErrOAuthNoAccount
	}

	if err != nil {
		return userdomain.User{}, err
	}

	if !user.IsActive() {
		return userdomain.User{}, authdomain.ErrUserInactive
	}

	return user, nil
}

func (s *AuthService) linkByEmail(ctx context.Context, identity authdomain.OAuthIdentity, now time.Time) (uuid.UUID, error) {
	email := strings.TrimSpace(strings.ToLower(identity.Email))
	if !identity.EmailVerified || email == "" {
		return uuid.Nil, authdomain.ErrOAuthNoAccount
	}

	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, userdomain.ErrNotFound) {
		return uuid.Nil, authdomain.ErrOAuthNoAccount
	}

	if err != nil {
		return uuid.Nil, err
	}

	if err := s.oauth.identities.Link(ctx, user.UUID, identity, now); err != nil {
		return uuid.Nil, err
	}

	return user.UUID, nil
}

func (s *AuthService) oauthProvider(name string) (ports.OAuthProvider, error) {
	p, ok := s.oauth.providers[name]
	if !ok || s.oauth.states == nil || s.oauth.identities == nil {
		return nil, authdomain.ErrOAuthProviderUnknown
	}

	return p, nil
}

// pkceChallenge is the RFC 7636 S256 challenge for verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
