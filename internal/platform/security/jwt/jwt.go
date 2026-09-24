// Package jwt issues and verifies RS256 access tokens and opaque refresh/reset tokens.
package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

// Service implements authports.TokenService.
type Service struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	hashKey    []byte
	issuer     string
}

// New builds an RS256 token service.
// privatePEM / publicPEM are PKCS#1 or PKCS#8 / PKIX PEM blocks.
// hashKey peppers opaque refresh and reset token hashes (APP_SESSION_KEY).
func New(privatePEM, publicPEM, hashKey, issuer string) (*Service, error) {
	if len(hashKey) < 32 {
		return nil, errors.New("JWT hash key must be at least 32 bytes")
	}

	privateKey, err := parseRSAPrivateKey([]byte(privatePEM))
	if err != nil {
		return nil, fmt.Errorf("parse JWT private key: %w", err)
	}

	publicKey, err := parseRSAPublicKey([]byte(publicPEM))
	if err != nil {
		return nil, fmt.Errorf("parse JWT public key: %w", err)
	}

	if privateKey.N.Cmp(publicKey.N) != 0 || privateKey.E != publicKey.E {
		return nil, errors.New("JWT public key does not match private key")
	}

	if issuer == "" {
		issuer = "blog-api"
	}

	return &Service{
		privateKey: privateKey,
		publicKey:  publicKey,
		hashKey:    []byte(hashKey),
		issuer:     issuer,
	}, nil
}

type accessClaims struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	jwtlib.RegisteredClaims
}

// IssueAccess signs an RS256 access token for the user.
func (s *Service) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, accessClaims{
		Email:    claims.Email,
		Username: claims.Username,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   claims.Subject.String(),
			Issuer:    s.issuer,
			ID:        claims.ID,
			IssuedAt:  jwtlib.NewNumericDate(claims.IssuedAt),
			ExpiresAt: jwtlib.NewNumericDate(claims.ExpiresAt),
		},
	})

	return token.SignedString(s.privateKey)
}

// ParseAccess verifies an access token's signature, issuer, and expiry.
func (s *Service) ParseAccess(token string) (authdomain.AccessClaims, error) {
	parsed, err := jwtlib.ParseWithClaims(token, &accessClaims{}, func(t *jwtlib.Token) (any, error) {
		if t.Method != jwtlib.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}

		return s.publicKey, nil
	})
	if err != nil || !parsed.Valid {
		return authdomain.AccessClaims{}, authdomain.ErrInvalidToken
	}

	claims, ok := parsed.Claims.(*accessClaims)
	if !ok {
		return authdomain.AccessClaims{}, authdomain.ErrInvalidToken
	}

	subject, err := uuid.Parse(claims.Subject)
	if err != nil {
		return authdomain.AccessClaims{}, authdomain.ErrInvalidToken
	}

	var exp, iat time.Time
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Time
	}

	if claims.IssuedAt != nil {
		iat = claims.IssuedAt.Time
	}

	return authdomain.AccessClaims{
		Subject:   subject,
		Email:     claims.Email,
		Username:  claims.Username,
		ExpiresAt: exp,
		IssuedAt:  iat,
		ID:        claims.ID,
	}, nil
}

// IssueRefresh returns a random refresh token and its keyed hash.
func (s *Service) IssueRefresh() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, s.HashRefresh(raw), nil
}

// HashRefresh returns the keyed hash stored for a refresh token.
func (s *Service) HashRefresh(raw string) string {
	sum := sha256.Sum256(append(append([]byte("refresh:"), s.hashKey...), []byte(raw)...))
	return hex.EncodeToString(sum[:])
}

// IssueResetToken returns a random reset token, its keyed hash, and a jti.
func (s *Service) IssueResetToken() (raw, hash, jti string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}

	jtiBuf := make([]byte, 16)
	if _, err = rand.Read(jtiBuf); err != nil {
		return "", "", "", err
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)
	jti = hex.EncodeToString(jtiBuf)

	return raw, s.HashResetToken(raw), jti, nil
}

// HashResetToken returns the keyed hash stored for a reset token.
func (s *Service) HashResetToken(raw string) string {
	sum := sha256.Sum256(append(append([]byte("reset:"), s.hashKey...), []byte(raw)...))
	return hex.EncodeToString(sum[:])
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	return parseRSAKey(pemBytes, "private", x509.ParsePKCS8PrivateKey, x509.ParsePKCS1PrivateKey)
}

func parseRSAPublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	return parseRSAKey(pemBytes, "public", x509.ParsePKIXPublicKey, x509.ParsePKCS1PublicKey)
}

// parseRSAKey decodes the first PEM block, trying the generic container
// (PKCS#8 / PKIX) before the RSA-only PKCS#1 encoding.
func parseRSAKey[K *rsa.PrivateKey | *rsa.PublicKey](
	pemBytes []byte,
	kind string,
	generic func([]byte) (any, error),
	pkcs1 func([]byte) (K, error),
) (K, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	if key, err := generic(block.Bytes); err == nil {
		rsaKey, ok := key.(K)
		if !ok {
			return nil, fmt.Errorf("not an RSA %s key", kind)
		}

		return rsaKey, nil
	}

	if key, err := pkcs1(block.Bytes); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("unsupported %s key encoding", kind)
}
