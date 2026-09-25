package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const oauthRedirect = "https://app.example.com/oauth/callback"

// fakeProvider returns identity for code "good" and records the exchange inputs.
type fakeProvider struct {
	identity    authdomain.OAuthIdentity
	gotVerifier string
	gotRedirect string
}

func (*fakeProvider) AuthorizeURL(state, challenge, redirectURI string) string {
	return "https://provider.test/authorize?" + url.Values{
		"state": {state}, "code_challenge": {challenge}, "redirect_uri": {redirectURI},
	}.Encode()
}

func (p *fakeProvider) Exchange(_ context.Context, code, verifier, redirectURI string) (authdomain.OAuthIdentity, error) {
	p.gotVerifier, p.gotRedirect = verifier, redirectURI

	if code != "good" {
		return authdomain.OAuthIdentity{}, authdomain.ErrOAuthExchange
	}

	return p.identity, nil
}

type memOAuthStates struct {
	mu     sync.Mutex
	states map[string]authdomain.OAuthState
}

func (m *memOAuthStates) Save(_ context.Context, state string, value authdomain.OAuthState, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.states[state] = value

	return nil
}

func (m *memOAuthStates) Consume(_ context.Context, state string) (authdomain.OAuthState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	value, ok := m.states[state]
	if !ok {
		return authdomain.OAuthState{}, authdomain.ErrOAuthStateInvalid
	}

	delete(m.states, state)

	return value, nil
}

type memIdentities struct {
	mu      sync.Mutex
	links   map[string]uuid.UUID
	touched int
}

func (m *memIdentities) FindUser(_ context.Context, provider, subject string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.links[provider+"|"+subject]
	if !ok {
		return uuid.Nil, authdomain.ErrOAuthNoAccount
	}

	return id, nil
}

func (m *memIdentities) Link(_ context.Context, userID uuid.UUID, identity authdomain.OAuthIdentity, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, id := range m.links {
		if id == userID && strings.HasPrefix(key, identity.Provider+"|") {
			return authdomain.ErrOAuthLinkConflict
		}
	}

	m.links[identity.Provider+"|"+identity.Subject] = userID

	return nil
}

func (m *memIdentities) Touch(context.Context, string, string, time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.touched++

	return nil
}

type oauthFixture struct {
	*twoFactorFixture

	provider   *fakeProvider
	states     *memOAuthStates
	identities *memIdentities
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()

	f := &oauthFixture{
		twoFactorFixture: newTwoFactorFixture(t),
		provider: &fakeProvider{identity: authdomain.OAuthIdentity{
			Subject: "g-123", Email: tfEmail, EmailVerified: true,
		}},
		states:     &memOAuthStates{states: map[string]authdomain.OAuthState{}},
		identities: &memIdentities{links: map[string]uuid.UUID{}},
	}
	f.svc.WithOAuth(map[string]ports.OAuthProvider{"google": f.provider}, f.identities, f.states, []string{oauthRedirect})

	return f
}

// start begins a flow and returns the state the provider would echo back.
func (f *oauthFixture) start(t *testing.T, provider string) string {
	t.Helper()

	start, err := f.svc.OAuthStart(t.Context(), provider, oauthRedirect)
	require.NoError(t, err)

	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)

	return u.Query().Get("state")
}

func TestOAuthStartStoresStateWithPKCE(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)

	start, err := f.svc.OAuthStart(t.Context(), "google", oauthRedirect)
	require.NoError(t, err)
	require.Equal(t, f.clock.Now().Add(10*time.Minute), start.ExpiresAt)

	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)

	stored, ok := f.states.states[u.Query().Get("state")]
	require.True(t, ok)
	require.Equal(t, "google", stored.Provider)
	require.Equal(t, oauthRedirect, stored.RedirectURI)

	sum := sha256.Sum256([]byte(stored.CodeVerifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), u.Query().Get("code_challenge"))
}

func TestOAuthStartRejectsUnknownProviderAndRedirect(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)

	_, err := f.svc.OAuthStart(t.Context(), "gitlab", oauthRedirect)
	require.ErrorIs(t, err, authdomain.ErrOAuthProviderUnknown)

	_, err = f.svc.OAuthStart(t.Context(), "google", oauthRedirect+"/evil")
	require.ErrorIs(t, err, authdomain.ErrOAuthRedirectURI)

	_, err = f.svc.OAuthStart(t.Context(), "google", "https://evil.example.com/oauth/callback")
	require.ErrorIs(t, err, authdomain.ErrOAuthRedirectURI)
	require.Empty(t, f.states.states)
}

func TestOAuthUnconfiguredServiceReportsUnknownProvider(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	_, err := f.svc.OAuthStart(t.Context(), "google", oauthRedirect)
	require.ErrorIs(t, err, authdomain.ErrOAuthProviderUnknown)

	_, err = f.svc.OAuthCallback(t.Context(), "google", "good", "state", "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthProviderUnknown)
}

func TestOAuthCallbackLinksVerifiedEmailThenUsesLink(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)

	state := f.start(t, "google")
	res, err := f.svc.OAuthCallback(t.Context(), "google", "good", state, "ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotEmpty(t, res.Tokens.AccessToken)
	require.Equal(t, f.userID, f.identities.links["google|g-123"])
	require.Equal(t, oauthRedirect, f.provider.gotRedirect)
	require.NotEmpty(t, f.provider.gotVerifier)

	// The link now wins even if the provider email changes.
	f.provider.identity.Email = "someone-else@example.com"
	f.provider.identity.EmailVerified = false

	res, err = f.svc.OAuthCallback(t.Context(), "google", "good", f.start(t, "google"), "ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotEmpty(t, res.Tokens.AccessToken)
	require.Equal(t, 1, f.identities.touched)
}

func TestOAuthCallbackNeverCreatesAccounts(t *testing.T) {
	t.Parallel()

	cases := map[string]authdomain.OAuthIdentity{
		"unverified email": {Subject: "g-1", Email: tfEmail, EmailVerified: false},
		"unknown email":    {Subject: "g-2", Email: "stranger@example.com", EmailVerified: true},
		"no email":         {Subject: "g-3", EmailVerified: true},
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newOAuthFixture(t)
			f.provider.identity = identity

			_, err := f.svc.OAuthCallback(t.Context(), "google", "good", f.start(t, "google"), "ua", "127.0.0.1")
			require.ErrorIs(t, err, authdomain.ErrOAuthNoAccount)
			require.Empty(t, f.identities.links)
		})
	}
}

func TestOAuthCallbackStateIsSingleUseAndBoundToProvider(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)
	github := &fakeProvider{identity: f.provider.identity}
	f.svc.WithOAuth(map[string]ports.OAuthProvider{"google": f.provider, "github": github},
		f.identities, f.states, []string{oauthRedirect})

	state := f.start(t, "google")
	_, err := f.svc.OAuthCallback(t.Context(), "github", "good", state, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid)
	require.Empty(t, github.gotVerifier, "no exchange with a mismatched provider")

	_, err = f.svc.OAuthCallback(t.Context(), "google", "good", state, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid, "the mismatched attempt consumed the state")

	state = f.start(t, "google")
	_, err = f.svc.OAuthCallback(t.Context(), "google", "good", state, "ua", "127.0.0.1")
	require.NoError(t, err)

	_, err = f.svc.OAuthCallback(t.Context(), "google", "good", state, "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthStateInvalid)
}

func TestOAuthCallbackRejectsBadCode(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)

	_, err := f.svc.OAuthCallback(t.Context(), "google", "bad", f.start(t, "google"), "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthExchange)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 401, status)
}

func TestOAuthCallbackRequiresTwoFactor(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)
	f.enroll(t)

	res, err := f.svc.OAuthCallback(t.Context(), "google", "good", f.start(t, "google"), "ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, res.Challenge)
	require.Empty(t, res.Tokens.AccessToken)
}

func TestOAuthCallbackRejectsInactiveUser(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)
	user := f.users.byID[f.userID]
	user.Status = userdomain.StatusSuspended
	f.users.byID[f.userID] = user
	f.users.byEmail[user.Email] = user

	_, err := f.svc.OAuthCallback(t.Context(), "google", "good", f.start(t, "google"), "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrUserInactive)
}

func TestOAuthCallbackReportsLinkConflict(t *testing.T) {
	t.Parallel()

	f := newOAuthFixture(t)
	f.identities.links["google|other-subject"] = f.userID

	_, err := f.svc.OAuthCallback(t.Context(), "google", "good", f.start(t, "google"), "ua", "127.0.0.1")
	require.ErrorIs(t, err, authdomain.ErrOAuthLinkConflict)

	_, _, status := authservice.MapError(err)
	require.Equal(t, 409, status)
}
