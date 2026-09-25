package jwt_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
)

func TestIssueAndParseAccess(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)

	now := time.Now().UTC()
	subject := uuid.New()
	raw, err := svc.IssueAccess(authdomain.AccessClaims{
		Subject: subject, Email: "a@example.com", Username: "a",
		ExpiresAt: now.Add(time.Minute), IssuedAt: now, ID: uuid.NewString(),
	})
	require.NoError(t, err)

	claims, err := svc.ParseAccess(raw)
	require.NoError(t, err)
	require.Equal(t, subject, claims.Subject)
	require.Equal(t, "a@example.com", claims.Email)
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

func TestRefreshHashStable(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	raw, hash, err := svc.IssueRefresh()
	require.NoError(t, err)
	require.Equal(t, hash, svc.HashRefresh(raw))
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
