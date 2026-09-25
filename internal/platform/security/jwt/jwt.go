// Package jwt issues and verifies ES256 access tokens and opaque refresh/reset tokens.
package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
	hashKey    []byte
	issuer     string
}

// New builds an ES256 token service.
// privatePEM is PKCS#8 or SEC1; publicPEM is PKIX. Both keys must be P-256.
// hashKey peppers opaque refresh and reset token hashes (APP_SESSION_KEY).
func New(privatePEM, publicPEM, hashKey, issuer string) (*Service, error) {
	if len(hashKey) < 32 {
		return nil, errors.New("JWT hash key must be at least 32 bytes")
	}

	privateKey, err := parseECPrivateKey([]byte(privatePEM))
	if err != nil {
		return nil, fmt.Errorf("parse JWT private key: %w", err)
	}

	publicKey, err := parseECPublicKey([]byte(publicPEM))
	if err != nil {
		return nil, fmt.Errorf("parse JWT public key: %w", err)
	}

	if err := requireP256(privateKey.Curve); err != nil {
		return nil, err
	}

	if err := requireP256(publicKey.Curve); err != nil {
		return nil, err
	}

	if !privateKey.PublicKey.Equal(publicKey) {
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
	Email     string     `json:"email"`
	Username  string     `json:"username"`
	Act       *actClaims `json:"act,omitempty"`
	SessionID string     `json:"sid,omitempty"`
	jwtlib.RegisteredClaims
}

// actClaims is the RFC 8693 actor claim.
type actClaims struct {
	Subject string `json:"sub"`
}

// IssueAccess signs an ES256 access token for the user; claims.Actor makes it an
// impersonation token.
func (s *Service) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	var act *actClaims

	if claims.Actor != nil {
		if claims.SessionID == "" {
			return "", errors.New("impersonation token requires a session id")
		}

		act = &actClaims{Subject: claims.Actor.String()}
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodES256, accessClaims{
		Email:     claims.Email,
		Username:  claims.Username,
		Act:       act,
		SessionID: claims.SessionID,
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
		if t.Method != jwtlib.SigningMethodES256 {
			return nil, errors.New("unexpected signing method")
		}

		return s.publicKey, nil
	}, jwtlib.WithValidMethods([]string{jwtlib.SigningMethodES256.Alg()}),
		jwtlib.WithIssuer(s.issuer), jwtlib.WithExpirationRequired())
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

	actor, err := parseActor(claims)
	if err != nil {
		return authdomain.AccessClaims{}, err
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
		Actor:     actor,
		SessionID: claims.SessionID,
	}, nil
}

// parseActor rejects a malformed act claim, and a session id without one, so an
// impersonation token can never pass as an ordinary token or the other way round.
func parseActor(claims *accessClaims) (*uuid.UUID, error) {
	if claims.Act == nil {
		if claims.SessionID != "" {
			return nil, authdomain.ErrInvalidToken
		}

		return nil, nil
	}

	actor, err := uuid.Parse(claims.Act.Subject)
	if err != nil || claims.SessionID == "" || actor.String() == claims.Subject {
		return nil, authdomain.ErrInvalidToken
	}

	return &actor, nil
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

func parseECPrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an ECDSA private key")
		}

		return ecKey, nil
	}

	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	return nil, errors.New("unsupported private key encoding")
}

func parseECPublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("unsupported public key encoding")
	}

	ecKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("not an ECDSA public key")
	}

	return ecKey, nil
}

func requireP256(curve elliptic.Curve) error {
	if curve != elliptic.P256() {
		return errors.New("JWT key must use P-256 for ES256")
	}

	return nil
}
