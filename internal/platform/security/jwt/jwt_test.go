package jwt_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"maps"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
)

func TestIssueAndParseAccess(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)

	now := time.Now().UTC()
	subject, family := uuid.New(), uuid.New()
	raw, err := svc.IssueAccess(authdomain.AccessClaims{
		Subject: subject, Email: "a@example.com", Username: "a",
		ExpiresAt: now.Add(time.Minute), IssuedAt: now, ID: uuid.NewString(), FamilyID: family,
	})
	require.NoError(t, err)

	claims, err := svc.ParseAccess(raw)
	require.NoError(t, err)
	require.Equal(t, subject, claims.Subject)
	require.Equal(t, "a@example.com", claims.Email)
	require.Equal(t, family, claims.FamilyID)
}

func TestImpersonationTokenCarriesActorAndSession(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	now := time.Now().UTC()
	subject, actor := uuid.New(), uuid.New()

	raw, err := svc.IssueAccess(authdomain.AccessClaims{
		Subject: subject, ExpiresAt: now.Add(time.Minute), IssuedAt: now, Actor: &actor, SessionID: "s-1",
	})
	require.NoError(t, err)

	claims, err := svc.ParseAccess(raw)
	require.NoError(t, err)
	require.True(t, claims.Impersonating())
	require.Equal(t, actor, *claims.Actor)
	require.Equal(t, "s-1", claims.SessionID)

	_, err = svc.IssueAccess(authdomain.AccessClaims{Subject: subject, ExpiresAt: now.Add(time.Minute), Actor: &actor})
	require.Error(t, err, "an impersonation token needs a session id")

	_, err = svc.IssueAccess(authdomain.AccessClaims{
		Subject: subject, ExpiresAt: now.Add(time.Minute), Actor: &actor, SessionID: "s-1", FamilyID: uuid.New(),
	})
	require.Error(t, err, "an impersonation token is not a sign-in of its own")

	plain, err := svc.ParseAccess(mustIssue(t, svc, authdomain.AccessClaims{Subject: subject, ExpiresAt: now.Add(time.Minute)}))
	require.NoError(t, err)
	require.False(t, plain.Impersonating())
}

func TestParseAccessRejectsMalformedActClaims(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	svc, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey),
		"01234567890123456789012345678901", "blog-api")
	require.NoError(t, err)

	subject := uuid.NewString()
	registered := jwtlib.MapClaims{
		"sub": subject, "iss": "blog-api", "exp": time.Now().Add(time.Minute).Unix(),
	}

	for name, extra := range map[string]jwtlib.MapClaims{
		"sid without act":   {"sid": "s-1"},
		"act without sid":   {"act": map[string]any{"sub": uuid.NewString()}},
		"act not a uuid":    {"act": map[string]any{"sub": "admin"}, "sid": "s-1"},
		"actor is subject":  {"act": map[string]any{"sub": subject}, "sid": "s-1"},
		"act missing a sub": {"act": map[string]any{}, "sid": "s-1"},
		"fam not a uuid":    {"fam": "family"},
		"fam is nil uuid":   {"fam": uuid.Nil.String()},
		"fam with act":      {"act": map[string]any{"sub": uuid.NewString()}, "sid": "s-1", "fam": uuid.NewString()},
	} {
		claims := jwtlib.MapClaims{}
		maps.Copy(claims, registered)
		maps.Copy(claims, extra)

		raw, err := jwtlib.NewWithClaims(jwtlib.SigningMethodES256, claims).SignedString(priv)
		require.NoError(t, err)

		_, err = svc.ParseAccess(raw)
		require.ErrorIs(t, err, authdomain.ErrInvalidToken, name)
	}
}

func mustIssue(t *testing.T, svc *jwttoken.Service, claims authdomain.AccessClaims) string {
	t.Helper()

	raw, err := svc.IssueAccess(claims)
	require.NoError(t, err)

	return raw
}

func TestParseAccessRejectsForeignIssuerAndMissingExpiry(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	newService := func(issuer string) *jwttoken.Service {
		svc, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey),
			"01234567890123456789012345678901", issuer)
		require.NoError(t, err)

		return svc
	}

	now := time.Now().UTC()
	claims := authdomain.AccessClaims{Subject: uuid.New(), ExpiresAt: now.Add(time.Minute), IssuedAt: now}

	foreign, err := newService("other-service").IssueAccess(claims)
	require.NoError(t, err)

	_, err = newService("blog-api").ParseAccess(foreign)
	require.ErrorIs(t, err, authdomain.ErrInvalidToken, "same key, different issuer")

	noExpiry, err := jwtlib.NewWithClaims(jwtlib.SigningMethodES256, jwtlib.RegisteredClaims{
		Subject: uuid.NewString(), Issuer: "blog-api", IssuedAt: jwtlib.NewNumericDate(now),
	}).SignedString(priv)
	require.NoError(t, err)

	_, err = newService("blog-api").ParseAccess(noExpiry)
	require.ErrorIs(t, err, authdomain.ErrInvalidToken, "exp is required")
}

func TestParseAccessRejectsInvalidTokens(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	svc, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey),
		"01234567890123456789012345678901", "blog-api")
	require.NoError(t, err)

	now := time.Now().UTC()
	valid := jwtlib.MapClaims{"sub": uuid.NewString(), "iss": "blog-api", "exp": now.Add(time.Minute).Unix()}

	sign := func(method jwtlib.SigningMethod, key any, claims jwtlib.MapClaims) string {
		t.Helper()

		raw, err := jwtlib.NewWithClaims(method, claims).SignedString(key)
		require.NoError(t, err)

		return raw
	}

	good := sign(jwtlib.SigningMethodES256, priv, valid)
	tampered := good[:len(good)-4] + "AAAA"
	if tampered == good {
		tampered = good[:len(good)-4] + "BBBB"
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "garbage", token: "not.a.jwt"},
		{name: "tampered signature", token: tampered},
		{name: "signed by another key", token: sign(jwtlib.SigningMethodES256, generateP256(t), valid)},
		{
			name:  "alg none",
			token: sign(jwtlib.SigningMethodNone, jwtlib.UnsafeAllowNoneSignatureType, valid),
		},
		{
			name:  "HS256 keyed with the public key",
			token: sign(jwtlib.SigningMethodHS256, []byte(encodePublicPEM(t, &priv.PublicKey)), valid),
		},
		{
			name: "expired",
			token: sign(jwtlib.SigningMethodES256, priv, jwtlib.MapClaims{
				"sub": uuid.NewString(), "iss": "blog-api", "exp": now.Add(-time.Minute).Unix(),
			}),
		},
		{
			name: "subject not a uuid",
			token: sign(jwtlib.SigningMethodES256, priv, jwtlib.MapClaims{
				"sub": "admin", "iss": "blog-api", "exp": now.Add(time.Minute).Unix(),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := svc.ParseAccess(tt.token)
			require.ErrorIs(t, err, authdomain.ErrInvalidToken)
		})
	}
}

func TestParseAccessWithoutIssuedAt(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	svc, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey),
		"01234567890123456789012345678901", "blog-api")
	require.NoError(t, err)

	subject := uuid.New()
	raw, err := jwtlib.NewWithClaims(jwtlib.SigningMethodES256, jwtlib.MapClaims{
		"sub": subject.String(), "iss": "blog-api", "exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(priv)
	require.NoError(t, err)

	claims, err := svc.ParseAccess(raw)
	require.NoError(t, err)
	require.Equal(t, subject, claims.Subject)
	require.True(t, claims.IssuedAt.IsZero())
	require.False(t, claims.ExpiresAt.IsZero())
}

func TestRefreshHashStable(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	raw, hash, err := svc.IssueRefresh()
	require.NoError(t, err)
	require.Equal(t, hash, svc.HashRefresh(raw))
}

func TestIssueResetToken(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)

	raw, hash, jti, err := svc.IssueResetToken()
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)

	jtiBytes, err := hex.DecodeString(jti)
	require.NoError(t, err)
	assert.Len(t, jtiBytes, 16)

	assert.Equal(t, hash, svc.HashResetToken(raw))

	raw2, hash2, jti2, err := svc.IssueResetToken()
	require.NoError(t, err)
	assert.NotEqual(t, raw, raw2)
	assert.NotEqual(t, hash, hash2)
	assert.NotEqual(t, jti, jti2)
}

func TestHashResetToken(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	newService := func(hashKey string) *jwttoken.Service {
		svc, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey), hashKey, "blog-api")
		require.NoError(t, err)

		return svc
	}

	svc := newService("01234567890123456789012345678901")
	other := newService("abcdefghijabcdefghijabcdefghijab")

	hash := svc.HashResetToken("token")
	assert.Len(t, hash, 64)
	assert.Equal(t, hash, svc.HashResetToken("token"), "stable")
	assert.NotEqual(t, hash, svc.HashResetToken("token2"))
	assert.NotEqual(t, hash, other.HashResetToken("token"), "keyed by the hash key")
	assert.NotEqual(t, hash, svc.HashRefresh("token"), "reset and refresh hashes must not collide")
}

func TestRejectsMismatchedKeyPair(t *testing.T) {
	t.Parallel()

	privA := generateP256(t)
	privB := generateP256(t)

	_, err := jwttoken.New(
		encodePrivatePEM(t, privA),
		encodePublicPEM(t, &privB.PublicKey),
		"01234567890123456789012345678901",
		"blog-api",
	)
	require.Error(t, err)
}

func TestRejectsNonP256Key(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	_, err = jwttoken.New(
		encodePrivatePEM(t, priv),
		encodePublicPEM(t, &priv.PublicKey),
		"01234567890123456789012345678901",
		"blog-api",
	)
	require.Error(t, err)
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	const hashKey = "01234567890123456789012345678901"

	priv := generateP256(t)
	privPEM, pubPEM := encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey)

	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	edPrivDER, err := x509.MarshalPKCS8PrivateKey(edPriv)
	require.NoError(t, err)

	edPubDER, err := x509.MarshalPKIXPublicKey(edPub)
	require.NoError(t, err)

	pemOf := func(typ string, der []byte) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	}

	tests := []struct {
		name       string
		privatePEM string
		publicPEM  string
		hashKey    string
		wantErr    string
	}{
		{name: "short hash key", privatePEM: privPEM, publicPEM: pubPEM, hashKey: "short", wantErr: "at least 32 bytes"},
		{name: "private key not PEM", privatePEM: "garbage", publicPEM: pubPEM, hashKey: hashKey, wantErr: "no PEM block"},
		{
			name: "private key not ECDSA", privatePEM: pemOf("PRIVATE KEY", edPrivDER), publicPEM: pubPEM,
			hashKey: hashKey, wantErr: "not an ECDSA private key",
		},
		{
			name: "private key undecodable", privatePEM: pemOf("PRIVATE KEY", []byte("junk")), publicPEM: pubPEM,
			hashKey: hashKey, wantErr: "unsupported private key encoding",
		},
		{name: "public key not PEM", privatePEM: privPEM, publicPEM: "garbage", hashKey: hashKey, wantErr: "no PEM block"},
		{
			name: "public key undecodable", privatePEM: privPEM, publicPEM: pemOf("PUBLIC KEY", []byte("junk")),
			hashKey: hashKey, wantErr: "unsupported public key encoding",
		},
		{
			name: "public key not ECDSA", privatePEM: privPEM, publicPEM: pemOf("PUBLIC KEY", edPubDER),
			hashKey: hashKey, wantErr: "not an ECDSA public key",
		},
		{
			name: "public key not P-256", privatePEM: privPEM, publicPEM: encodePublicPEM(t, &p384.PublicKey),
			hashKey: hashKey, wantErr: "P-256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := jwttoken.New(tt.privatePEM, tt.publicPEM, tt.hashKey, "blog-api")
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, svc)
		})
	}
}

func TestNewAcceptsSEC1KeyAndDefaultsIssuer(t *testing.T) {
	t.Parallel()

	priv := generateP256(t)
	sec1, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	const hashKey = "01234567890123456789012345678901"

	svc, err := jwttoken.New(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})),
		encodePublicPEM(t, &priv.PublicKey), hashKey, "")
	require.NoError(t, err)

	named, err := jwttoken.New(encodePrivatePEM(t, priv), encodePublicPEM(t, &priv.PublicKey), hashKey, "blog-api")
	require.NoError(t, err)

	now := time.Now().UTC()
	subject := uuid.New()
	raw := mustIssue(t, svc, authdomain.AccessClaims{Subject: subject, ExpiresAt: now.Add(time.Minute), IssuedAt: now})

	claims, err := named.ParseAccess(raw)
	require.NoError(t, err, "an empty issuer defaults to blog-api")
	require.Equal(t, subject, claims.Subject)
}

func newTestService(t *testing.T) *jwttoken.Service {
	t.Helper()

	priv := generateP256(t)
	svc, err := jwttoken.New(
		encodePrivatePEM(t, priv),
		encodePublicPEM(t, &priv.PublicKey),
		"01234567890123456789012345678901",
		"blog-api",
	)
	require.NoError(t, err)

	return svc
}

func generateP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	return key
}

func encodePrivatePEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func encodePublicPEM(t *testing.T, key *ecdsa.PublicKey) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
