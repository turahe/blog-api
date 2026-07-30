package jwt_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	jwttoken "github.com/turahe/blog-api/internal/platform/security/jwt"
)

func TestIssueAndParseAccess(t *testing.T) {
	svc, err := jwttoken.New("01234567890123456789012345678901", "blog-api")
	require.NoError(t, err)

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

func TestRefreshHashStable(t *testing.T) {
	svc, err := jwttoken.New("01234567890123456789012345678901", "blog-api")
	require.NoError(t, err)
	raw, hash, err := svc.IssueRefresh()
	require.NoError(t, err)
	require.Equal(t, hash, svc.HashRefresh(raw))
}
