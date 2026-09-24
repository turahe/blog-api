package password_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/security/password"
)

func TestHashAndCompare(t *testing.T) {
	t.Parallel()

	hasher := password.New()
	encoded, err := hasher.Hash("correct horse battery staple")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$"))
	require.True(t, hasher.Compare(encoded, "correct horse battery staple"))
	require.False(t, hasher.Compare(encoded, "wrong password"))
	require.False(t, hasher.Compare("not-a-hash", "correct horse battery staple"))
}

func TestHashUsesUniqueSalt(t *testing.T) {
	t.Parallel()

	hasher := password.New()
	first, err := hasher.Hash("same-password")
	require.NoError(t, err)
	second, err := hasher.Hash("same-password")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}
