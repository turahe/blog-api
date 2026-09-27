package password_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/security/password"
	"golang.org/x/crypto/argon2"
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

func TestCompareHonoursStoredParameters(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")
	sum := argon2.IDKey([]byte("pw"), salt, 1, 8*1024, 1, 32)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=8192,t=1,p=1$%s$%s", argon2.Version,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum))

	hasher := password.New()
	assert.True(t, hasher.Compare(encoded, "pw"))
	assert.False(t, hasher.Compare(encoded, "other"))
}

func TestCompareRejectsMalformedHashes(t *testing.T) {
	t.Parallel()

	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	sum := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	hash := func(version, cost, salt, sum string) string {
		return "$argon2id$" + version + "$" + cost + "$" + salt + "$" + sum
	}

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "wrong algorithm", encoded: "$argon2i$v=19$m=65536,t=3,p=2$" + salt + "$" + sum},
		{name: "too few segments", encoded: "$argon2id$v=19$m=65536,t=3,p=2$" + salt},
		{name: "unparseable version", encoded: hash("version", "m=65536,t=3,p=2", salt, sum)},
		{name: "unsupported version", encoded: hash("v=16", "m=65536,t=3,p=2", salt, sum)},
		{name: "unparseable parameters", encoded: hash("v=19", "m=lots", salt, sum)},
		{name: "time cost zero", encoded: hash("v=19", "m=65536,t=0,p=2", salt, sum)},
		{name: "time cost too high", encoded: hash("v=19", "m=65536,t=11,p=2", salt, sum)},
		{name: "memory too low", encoded: hash("v=19", "m=1024,t=3,p=2", salt, sum)},
		{name: "memory too high", encoded: hash("v=19", "m=1048576,t=3,p=2", salt, sum)},
		{name: "threads zero", encoded: hash("v=19", "m=65536,t=3,p=0", salt, sum)},
		{name: "threads too high", encoded: hash("v=19", "m=65536,t=3,p=5", salt, sum)},
		{name: "salt not base64", encoded: hash("v=19", "m=65536,t=3,p=2", "!!!", sum)},
		{name: "salt too short", encoded: hash("v=19", "m=65536,t=3,p=2", base64.RawStdEncoding.EncodeToString([]byte("short")), sum)},
		{name: "salt too long", encoded: hash("v=19", "m=65536,t=3,p=2", base64.RawStdEncoding.EncodeToString(make([]byte, 65)), sum)},
		{name: "sum not base64", encoded: hash("v=19", "m=65536,t=3,p=2", salt, "!!!")},
		{name: "sum wrong length", encoded: hash("v=19", "m=65536,t=3,p=2", salt, base64.RawStdEncoding.EncodeToString(make([]byte, 16)))},
	}

	hasher := password.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.False(t, hasher.Compare(tt.encoded, "pw"))
		})
	}
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
