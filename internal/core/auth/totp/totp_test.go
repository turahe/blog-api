package totp

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RFC 6238 Appendix B vectors (SHA-1, truncated to 6 digits).
func TestCodeMatchesRFC6238Vectors(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")
	cases := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}

	for unix, want := range cases {
		require.Equal(t, want, Code(secret, Step(time.Unix(unix, 0))), unix)
	}
}

func TestValidateAcceptsSkewAndReturnsStep(t *testing.T) {
	t.Parallel()

	secret, err := NewSecret()
	require.NoError(t, err)

	now := time.Unix(1_700_000_000, 0)
	prev := Code(secret, Step(now)-1)

	step, ok := Validate(secret, prev, now, 1)
	require.True(t, ok)
	require.Equal(t, Step(now)-1, step)

	_, ok = Validate(secret, Code(secret, Step(now)-2), now, 1)
	require.False(t, ok)

	_, ok = Validate(secret, "12345", now, 1)
	require.False(t, ok)
}

func TestURL(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")
	raw := URL("Blog API", "ada@example.com", secret)
	require.True(t, strings.HasPrefix(raw, "otpauth://totp/"))

	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, Encode(secret), u.Query().Get("secret"))
	require.Equal(t, "Blog API", u.Query().Get("issuer"))
	require.Equal(t, "/Blog API:ada@example.com", u.Path)
}
