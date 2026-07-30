package jwt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

type Service struct {
	secret []byte
	issuer string
}

func New(secret string, issuer string) (*Service, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT signing secret must be at least 32 bytes")
	}
	if issuer == "" {
		issuer = "blog-api"
	}
	return &Service{secret: []byte(secret), issuer: issuer}, nil
}

type accessClaims struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	jwtlib.RegisteredClaims
}

func (s *Service) IssueAccess(claims authdomain.AccessClaims) (string, error) {
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, accessClaims{
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
	return token.SignedString(s.secret)
}

func (s *Service) ParseAccess(token string) (authdomain.AccessClaims, error) {
	parsed, err := jwtlib.ParseWithClaims(token, &accessClaims{}, func(t *jwtlib.Token) (any, error) {
		if t.Method != jwtlib.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
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

func (s *Service) IssueRefresh() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, s.HashRefresh(raw), nil
}

func (s *Service) HashRefresh(raw string) string {
	sum := sha256.Sum256(append(append([]byte("refresh:"), s.secret...), []byte(raw)...))
	return hex.EncodeToString(sum[:])
}

func (s *Service) IssueResetToken() (raw string, hash string, jti string, err error) {
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

func (s *Service) HashResetToken(raw string) string {
	sum := sha256.Sum256(append(append([]byte("reset:"), s.secret...), []byte(raw)...))
	return hex.EncodeToString(sum[:])
}
