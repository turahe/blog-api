package domain

import (
	"errors"
	"time"
)

// OAuth errors; handlers map them via service.MapError.
var (
	ErrOAuthProviderUnknown = errors.New("oauth provider is not configured")
	ErrOAuthRedirectURI     = errors.New("redirect_uri is not allowed")
	ErrOAuthStateInvalid    = errors.New("oauth state is invalid or expired")
	ErrOAuthExchange        = errors.New("oauth provider rejected the authorization code")
	ErrOAuthNoAccount       = errors.New("no account is linked to this identity")
	ErrOAuthLinkConflict    = errors.New("account is already linked to another identity at this provider")
)

// OAuthIdentity is the account a provider vouches for after a code exchange.
type OAuthIdentity struct {
	Provider      string
	Subject       string // stable provider user id
	Email         string
	EmailVerified bool
}

// OAuthState is the server side of one authorization request, keyed by its state value.
type OAuthState struct {
	Provider     string
	RedirectURI  string
	CodeVerifier string // PKCE
}

// OAuthStart is returned by the start step; the client redirects to AuthorizeURL.
type OAuthStart struct {
	AuthorizeURL string
	ExpiresAt    time.Time
}
